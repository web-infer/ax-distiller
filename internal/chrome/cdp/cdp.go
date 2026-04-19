// reimplement CDP commands for performance

package cdp

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

type Request[T any] interface {
	ProtoReq() string
	Call(proto.Client) (*T, error)
}

func CommandOutputPtr[I Request[O], O any](ctx context.Context, page *rod.Page, req I, res *O) (err error) {
	if ctx == context.TODO() {
		ctx = page.GetContext()
	}
	resBuff, err := page.Call(ctx, string(page.SessionID), req.ProtoReq(), req)
	if err != nil {
		err = fmt.Errorf("%s: %w", req.ProtoReq(), err)
		return
	}
	err = sonic.ConfigFastest.Unmarshal(resBuff, res)
	if err != nil {
		err = fmt.Errorf("%s: %w", req.ProtoReq(), err)
		return
	}
	return
}

func Command[I Request[O], O any](ctx context.Context, page *rod.Page, req I) (res O, err error) {
	err = CommandOutputPtr(ctx, page, req, &res)
	return
}

type RequestUnary interface {
	ProtoReq() string
	Call(proto.Client) error
}

func CommandUnary[I RequestUnary](ctx context.Context, page *rod.Page, req I) (err error) {
	if ctx == context.TODO() {
		ctx = page.GetContext()
	}
	_, err = page.Call(ctx, string(page.SessionID), req.ProtoReq(), req)
	if err != nil {
		err = fmt.Errorf("%s: %w", req.ProtoReq(), err)
		return
	}
	return
}
