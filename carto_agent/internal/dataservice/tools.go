package dataservice

import (
	"context"
	"encoding/json"
	"fmt"
)

type serviceKey struct{}

func WithService(ctx context.Context, service *Service) context.Context {
	return context.WithValue(ctx, serviceKey{}, service)
}
func FromContext(ctx context.Context) *Service {
	value, _ := ctx.Value(serviceKey{}).(*Service)
	return value
}
func Handles(name string) bool {
	return name == "data_find" || name == "data_get" || name == "data_register"
}
func Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	s := FromContext(ctx)
	if s == nil {
		return nil, fmt.Errorf("当前会话的数据服务尚未装配")
	}
	return s.Invoke(ctx, name, args)
}
func (s *Service) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	switch name {
	case "data_find":
		var r FindRequest
		if err = s.decodeArguments(name, raw, &r); err != nil {
			return nil, err
		}
		return s.Find(ctx, r)
	case "data_get":
		var r GetRequest
		if err = s.decodeArguments(name, raw, &r); err != nil {
			return nil, err
		}
		return s.Get(ctx, r)
	case "data_register":
		var r RegisterRequest
		if err = s.decodeArguments(name, raw, &r); err != nil {
			return nil, err
		}
		return s.Register(ctx, r)
	default:
		return nil, fmt.Errorf("未知数据服务工具：%s", name)
	}
}
