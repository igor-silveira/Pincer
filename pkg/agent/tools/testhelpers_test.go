package tools

import (
	"context"

	"github.com/igorsilveira/pincer/pkg/sandbox"
)

type fakeSandbox struct {
	result *sandbox.Result
	err    error
	gotCmd sandbox.Command
}

func (f *fakeSandbox) Exec(_ context.Context, cmd sandbox.Command, _ sandbox.Policy) (*sandbox.Result, error) {
	f.gotCmd = cmd
	return f.result, f.err
}
