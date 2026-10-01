package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"sys-backend/config"

	"github.com/stretchr/testify/assert"
)

// restoreAstraConfig 临时替换 Astra 后端地址，测试结束后还原，避免污染同包其它用例。
func restoreAstraConfig(t *testing.T, url, secret string) {
	t.Helper()
	old := config.Configs.Astra
	config.Configs.Astra = config.AstraConfig{URL: url, Token: old.Token, InternalSecret: secret}
	t.Cleanup(func() { config.Configs.Astra = old })
}

// 出站请求必须带 AstraSchedule/System：WAF 会对非标 UA 发起 JS 质询，请求根本到不了后端。
func TestCallAstraDropTableSendsUserAgent(t *testing.T) {
	ensureTestDB()

	var gotUA, gotSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotSecret = r.Header.Get("X-Internal-Secret")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":200}`))
	}))
	defer srv.Close()

	restoreAstraConfig(t, srv.URL, "test-internal-secret")

	assert.NoError(t, callAstraDropTable("schedules"))
	assert.Equal(t, astraBackendUserAgent, gotUA)
	assert.Equal(t, "test-internal-secret", gotSecret)
}

// 媒体类型大小写不敏感，参数不能靠子串匹配：
// Application/JSON 必须判成功，text/html; note="application/json" 必须判失败。
func TestCallAstraDropTableContentTypeParsing(t *testing.T) {
	ensureTestDB()

	cases := []struct {
		name        string
		contentType string
		wantErr     bool
	}{
		{"小写媒体类型", "application/json", false},
		{"大写媒体类型带参数", "Application/JSON; charset=utf-8", false},
		{"HTML 参数里带 application/json", `text/html; note="application/json"`, true},
		{"WAF 质询页", "text/html; charset=utf-8", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(`{"status":200}`))
			}))
			defer srv.Close()

			restoreAstraConfig(t, srv.URL, "")

			err := callAstraDropTable("schedules")
			if tc.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "非 JSON")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// WAF 的 JS 质询返回 200 + HTML，只判断状态码会把“被拦下”当成“删表成功”。
func TestCallAstraDropTableRejectsChallengedResponse(t *testing.T) {
	ensureTestDB()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><script>var arg1='challenge'</script></html>`))
	}))
	defer srv.Close()

	restoreAstraConfig(t, srv.URL, "")

	err := callAstraDropTable("schedules")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "非 JSON")
}
