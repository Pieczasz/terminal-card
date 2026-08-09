package db

import "embed"

// Migrations is the same SQL golang-migrate applies in production. Tests apply it
// directly so the schema they run against cannot drift from the deployed one.
//
//go:embed migrations/*.sql
var Migrations embed.FS
