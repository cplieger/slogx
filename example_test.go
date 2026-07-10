package slogx_test

import (
	"fmt"
	"log/slog"

	"github.com/cplieger/slogx"
)

func ExampleParseLevel() {
	lvl, ok := slogx.ParseLevel("warning", slog.LevelInfo)
	fmt.Println(lvl, ok)

	_, ok = slogx.ParseLevel("banana", slog.LevelInfo)
	fmt.Println(ok)
	// Output:
	// WARN true
	// false
}
