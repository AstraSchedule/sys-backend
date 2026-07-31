package middleware

import (
	"sync"
	"time"
)

// LoginRateLimiter 登录接口的简单内存限流（按 IP+用户名维度），防止暴力破解。
// 仅统计失败尝试，成功登录即重置，避免误伤同一出口 IP 下的正常用户。
type LoginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

type loginAttempt struct {
	failures  int
	windowEnd time.Time
}

const (
	loginMaxFailures = 10               // 窗口内允许的最大失败次数
	loginWindow      = 15 * time.Minute // 计数窗口
)

var LoginLimiter = &LoginRateLimiter{attempts: map[string]*loginAttempt{}}

func loginKey(ip, username string) string { return ip + "|" + username }

// Allow 检查当前 IP+用户名是否仍允许尝试登录
func (l *LoginRateLimiter) Allow(ip, username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := loginKey(ip, username)
	a, ok := l.attempts[k]
	if !ok {
		return true
	}
	if time.Now().After(a.windowEnd) {
		delete(l.attempts, k)
		return true
	}
	return a.failures < loginMaxFailures
}

// Fail 记录一次失败的登录尝试
func (l *LoginRateLimiter) Fail(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := loginKey(ip, username)
	a, ok := l.attempts[k]
	if !ok || time.Now().After(a.windowEnd) {
		l.attempts[k] = &loginAttempt{failures: 1, windowEnd: time.Now().Add(loginWindow)}
		return
	}
	a.failures++
}

// Reset 登录成功后清空该 IP+用户名的失败计数
func (l *LoginRateLimiter) Reset(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, loginKey(ip, username))
}
