package cli

import (
	"context"
	"io"
	"os"
)

// OpenInputFile opens user-selected input without waiting for a FIFO writer.
// The caller owns the returned file and must close it after ReadInput returns.
func OpenInputFile(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openInputFile(path)
}

// ReadInput reads at most limit bytes, checking cancellation before returning
// data. It exclusively consumes input for this operation. Native files use
// platform cancellation without closing borrowed stdin. Other blocking inputs
// must implement io.ReadCloser with Close unblocking Read; cancellation takes
// ownership of closing them. Non-closable readers are for in-memory input.
func ReadInput(ctx context.Context, input io.Reader, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if file, ok := input.(*os.File); ok {
		return readFileInput(ctx, file, limit)
	}
	finish := func() {}
	if closer, ok := input.(io.ReadCloser); ok {
		done := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			_ = closer.Close()
			close(done)
		})
		finish = func() {
			if !stop() {
				<-done
			}
		}
	}
	defer finish()
	return readInput(ctx, input, limit)
}

func readInput(ctx context.Context, input io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(&contextInput{ctx: ctx, input: input}, limit))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return raw, err
}

type contextInput struct {
	ctx   context.Context
	input io.Reader
}

func (r *contextInput) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}
