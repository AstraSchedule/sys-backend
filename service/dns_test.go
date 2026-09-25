package service

import (
	"testing"

	"sys-backend/config"
)

// 租户识别是本次迁移最容易出错的一处：历史记录没有备注，只能靠 namespace 兜底；
// 基础设施记录两条判据都不满足，必须被排除，否则会在租户列表里冒出 class/to/sys。
func TestIsTenantRecord(t *testing.T) {
	namespaces := map[string]bool{subdomainToNamespace("kuohu"): true}

	cases := []struct {
		name      string
		comment   string
		subdomain string
		marker    string
		want      bool
	}{
		{"备注标记命中", "SaaS", "brandnew", "SaaS", true},
		{"备注大小写不敏感", "saas", "brandnew", "SaaS", true},
		{"备注含标记即可", "SaaS 租户", "brandnew", "SaaS", true},
		{"反向说明不算标记", "non-SaaS", "brandnew", "SaaS", false},
		{"标记不在开头不算", "租户 SaaS", "brandnew", "SaaS", false},
		{"无备注但库里有 namespace（历史记录）", "", "kuohu", "SaaS", true},
		{"基础设施记录：无备注且库里没有", "", "class", "SaaS", false},
		{"无备注且子域名不存在", "", "brandnew", "SaaS", false},
		{"未配置标记时只认 namespace", "SaaS", "brandnew", "", false},
		{"未配置标记但库里有", "", "kuohu", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTenantRecord(tc.comment, tc.subdomain, namespaces, tc.marker); got != tc.want {
				t.Fatalf("isTenantRecord(%q, %q) = %v，期望 %v", tc.comment, tc.subdomain, got, tc.want)
			}
		})
	}
}

func TestSubdomainOf(t *testing.T) {
	previous := config.Configs.ESA.SiteName
	config.Configs.ESA.SiteName = "getastra.cn"
	t.Cleanup(func() { config.Configs.ESA.SiteName = previous })

	cases := []struct {
		recordName string
		want       string
		wantOK     bool
	}{
		{"kuohu.getastra.cn", "kuohu", true},
		{"a.b.getastra.cn", "a.b", true},
		{"getastra.cn", "", false},
		{"kuohu.example.com", "", false},
	}

	for _, tc := range cases {
		got, ok := subdomainOf(tc.recordName)
		if got != tc.want || ok != tc.wantOK {
			t.Fatalf("subdomainOf(%q) = (%q, %v)，期望 (%q, %v)", tc.recordName, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestValidateSubdomain(t *testing.T) {
	valid := []string{"school", "nj39", "my-school", "a"}
	for _, s := range valid {
		if err := validateSubdomain(s); err != nil {
			t.Fatalf("%q 应通过校验，实际报错: %v", s, err)
		}
	}

	invalid := []string{"", "School", "-school", "school-", "my_school", "a.b", "*.getastra"}
	for _, s := range invalid {
		if err := validateSubdomain(s); err == nil {
			t.Fatalf("%q 应被拒绝", s)
		}
	}
}
