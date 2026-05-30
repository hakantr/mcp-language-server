package lsp

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type bufferedWriteCloser struct {
	bytes.Buffer
}

func (w *bufferedWriteCloser) Close() error {
	return nil
}

func TestCallReturnsWhenContextIsCanceled(t *testing.T) {
	writer := &bufferedWriteCloser{}
	client := &Client{
		stdin:    writer,
		handlers: make(map[string]chan *Message),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := client.Call(ctx, "workspace/symbol", map[string]any{"query": "symbol"}, nil)

	require.Error(t, err)
	require.ErrorContains(t, err, "request workspace/symbol canceled")
	require.Empty(t, client.handlers)
	require.NotEmpty(t, writer.String())
}

func TestNotifyReturnsWhenContextIsCanceled(t *testing.T) {
	writer := &bufferedWriteCloser{}
	client := &Client{stdin: writer}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.Notify(ctx, "initialized", struct{}{})

	require.Error(t, err)
	require.ErrorContains(t, err, "notification initialized canceled")
	require.Empty(t, writer.String())
}
