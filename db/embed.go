package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS

//go:embed seeders/*.sql
var Seeders embed.FS
