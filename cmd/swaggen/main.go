//go:build ignore

package main

import (
	"log"
	"os"

	"github.com/swaggo/swag"
	"github.com/swaggo/swag/gen"
)

func main() {
	err := os.MkdirAll("docs/api", 0755)
	if err != nil {
		log.Fatal(err)
	}

	cfg := &gen.Config{
		SearchDir:          ".",
		MainAPIFile:        "cmd/aether/main.go",
		OutputDir:          "docs/api",
		OutputTypes:        []string{"json", "yaml"},
		ParseDependency:    1,
		ParseInternal:      true,
		PropNamingStrategy: swag.CamelCase,
	}

	if err := gen.New().Build(cfg); err != nil {
		log.Fatalf("swag gen failed: %v", err)
	}
	log.Println("swagger spec generated successfully")
}
