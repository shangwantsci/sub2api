//go:build unit

package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 供号商相关的仓储方法在既有测试替身上的空实现。

func (m *stubUserRepo) ListProviders(context.Context) ([]service.User, error) { return nil, nil }
