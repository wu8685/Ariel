package appserver

import "context"

type CreationEndpoint interface {
	RPC
	Close() error
}

type ThreadCreator struct {
	Open func(context.Context, string) (CreationEndpoint, error)
}

func NewThreadCreator(binary string) ThreadCreator {
	return ThreadCreator{Open: func(ctx context.Context, cwd string) (CreationEndpoint, error) {
		return Start(ctx, binary, cwd)
	}}
}

func (c ThreadCreator) Create(ctx context.Context, cwd string) (Thread, error) {
	if c.Open == nil || cwd == "" {
		return Thread{}, ErrInvalidArgument
	}
	endpoint, err := c.Open(ctx, cwd)
	if err != nil {
		return Thread{}, err
	}
	defer endpoint.Close()
	var out struct {
		Thread Thread `json:"thread"`
	}
	err = endpoint.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "ephemeral": false, "serviceName": "ariel"}, &out)
	if err != nil {
		switch err {
		case ErrInvalidArgument, ErrMethodUnavailable, ErrRemote:
			return Thread{}, err
		default:
			return Thread{}, ErrOutcomeUnknown
		}
	}
	if out.Thread.ID == "" || out.Thread.CWD != cwd {
		return Thread{}, ErrOutcomeUnknown
	}
	return out.Thread, nil
}
