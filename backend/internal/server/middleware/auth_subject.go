package middleware

import "github.com/gin-gonic/gin"

// AuthSubject is the minimal authenticated identity stored in gin context.
// Decision: {UserID int64, Concurrency int}
type AuthSubject struct {
	UserID      int64
	Concurrency int
}

func GetAuthSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	value, exists := c.Get(string(ContextKeyUser))
	if !exists {
		return AuthSubject{}, false
	}
	subject, ok := value.(AuthSubject)
	return subject, ok
}

func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	value, exists := c.Get(string(ContextKeyUserRole))
	if !exists {
		return "", false
	}
	role, ok := value.(string)
	return role, ok
}

// IsProviderFromContext 报告当前请求主体是否为供号商。
// 未认证或键缺失时返回 false。
func IsProviderFromContext(c *gin.Context) bool {
	value, exists := c.Get(string(ContextKeyIsProvider))
	if !exists {
		return false
	}
	isProvider, ok := value.(bool)
	return ok && isProvider
}
