package lobby

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/observability"
)

// finalizeRequest is the lobby-side snapshot a finished game needs to persist.
// Lobby gathers it under its lock; Manager owns the write.
type finalizeRequest struct {
	lobbyCode string
	gameName  string
	isRanked  bool
	startedAt time.Time
}

// finalizeFinishedGame persists the result of a game that just ended.
// registerFinalizer runs before anything else: every statement between observing
// the end and that call is a window for shutdown to begin, and a refusal then
// drops a finished match with nothing left for WaitForFinalizers to wait on.
func (m *Manager) finalizeFinishedGame(req finalizeRequest, engine *game.Engine, reason game.EndReason) {
	if m == nil || m.matchRepo == nil {
		return
	}
	registered := m.registerFinalizer()
	parentCtx := m.shutdownCtx()

	if !registered {
		slog.ErrorContext(parentCtx, "finished match dropped; shutdown stopped new finalizers",
			"lobby", req.lobbyCode, "game", req.gameName, "ranked", req.isRanked)
		observability.MatchFinalize(parentCtx, "dropped", req.isRanked)
		return
	}
	defer m.finalizing.Done()

	if !req.startedAt.IsZero() {
		observability.GameFinished(parentCtx, req.gameName, req.isRanked, endReasonLabel(reason), time.Since(req.startedAt))
	}
	if req.gameName == "" {
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, rankedFinalizeTimeout)
	defer cancel()
	m.persistFinishedMatch(ctx, engine, reason, req)
}

func endReasonLabel(reason game.EndReason) string {
	switch reason {
	case game.EndReasonWin:
		return "win"
	case game.EndReasonRulesError:
		return "rules_error"
	case game.EndReasonForfeit:
		return "forfeit"
	case game.EndReasonAbandoned:
		return "abandoned"
	case game.EndReasonUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

func (m *Manager) persistFinishedMatch(
	ctx context.Context, engine *game.Engine, reason game.EndReason, req finalizeRequest,
) {
	standings, places := engine.StandingsWithPlaces()
	if reason == game.EndReasonAbandoned && len(standings) == 0 {
		return
	}

	userIDs := make([]uint, 0, len(standings))
	for i, p := range standings {
		if p == nil || p.UserID == 0 {
			slog.ErrorContext(ctx, "standing player has no database user; match not recorded",
				"lobby", req.lobbyCode, "game", req.gameName, "ranked", req.isRanked, "player_index", i)
			observability.MatchFinalize(ctx, "dropped", req.isRanked)
			return
		}
		userIDs = append(userIDs, p.UserID)
	}

	// A match the deploy interrupted has no honest winner: SSH teardown order, not
	// play, decided who was left holding cards. Rules errors are the same class —
	// half-applied state must not move the ladder.
	rated := req.isRanked && !m.isShuttingDown() && reason != game.EndReasonRulesError
	if req.isRanked && !rated {
		if reason == game.EndReasonRulesError {
			slog.WarnContext(ctx, "rules error ended the match; recording without Elo",
				"lobby", req.lobbyCode, "game", req.gameName)
		} else {
			slog.WarnContext(ctx, "server is shutting down; recording the ranked match without Elo",
				"lobby", req.lobbyCode, "game", req.gameName)
		}
	}

	if err := m.recordFinishedMatch(ctx, req.gameName, userIDs, places, rated); err != nil {
		slog.ErrorContext(ctx, "failed to record finished match",
			"error", err, "lobby", req.lobbyCode, "game", req.gameName, "ranked", rated)
		observability.MatchFinalize(ctx, "error", req.isRanked)
		return
	}
	observability.MatchFinalize(ctx, "ok", req.isRanked)
}

func (m *Manager) recordFinishedMatch(
	ctx context.Context, gameName string, userIDs []uint, places []int, isRanked bool,
) error {
	if isRanked {
		if err := m.matchRepo.FinalizeRankedMatch(ctx, gameName, userIDs, places); err != nil {
			return fmt.Errorf("finalize ranked match: %w", err)
		}
		return nil
	}
	if err := m.matchRepo.RecordCasualMatch(ctx, gameName, userIDs); err != nil {
		return fmt.Errorf("record casual match: %w", err)
	}
	return nil
}
