//go:build unit

package service_test

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 外部测试包（service_test）里的替身上的供号商方法空实现。
// 同包内的替身见 provider_repo_stubs_unit_test.go。

func (s *emailBindUserRepoStub) ListProviders(context.Context) ([]service.User, error) {
	return nil, nil
}
