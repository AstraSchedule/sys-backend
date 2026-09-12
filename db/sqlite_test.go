package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"sys-backend/config"
)

// closeTestConn 关闭测试连接，确保数据真正落盘
func closeTestConn(t *testing.T, conn *gorm.DB) {
	t.Helper()
	sqlDB, err := conn.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

// newWALDatabase 造一个 WAL 模式的库：建库写入数据后关闭连接。
// journal_mode 持久化在库头里，关闭后文件依然是 WAL。
func newWALDatabase(t *testing.T, path string) {
	t.Helper()
	conn, err := gorm.Open(gormsqlite.Open(path + "?_pragma=journal_mode(WAL)"))
	require.NoError(t, err)
	require.NoError(t, conn.Exec("CREATE TABLE t (a integer)").Error)
	require.NoError(t, conn.Exec("INSERT INTO t VALUES (1)").Error)
	closeTestConn(t, conn)
}

// journalModeOf 读取连接上实际生效的 journal 模式
func journalModeOf(t *testing.T, conn *gorm.DB) string {
	t.Helper()
	var mode string
	require.NoError(t, conn.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	return mode
}

// mustFormatVersion 读取库头里的文件格式版本
func mustFormatVersion(t *testing.T, path string) byte {
	t.Helper()
	version, err := sqliteFormatVersion(path)
	require.NoError(t, err)
	return version
}

func TestEnsureRollbackJournal_AllowsNewDatabase(t *testing.T) {
	assert.NoError(t, ensureRollbackJournal(filepath.Join(t.TempDir(), "new.db")))
	assert.NoError(t, ensureRollbackJournal(":memory:"))
}

// TestEnsureRollbackJournal_ConvertsLegacyWALDatabase 复现线上事故：库一旦被写成 WAL，之后不带任何
// pragma 打开也仍然是 WAL（该属性持久化在库头里），而 WAL 在 NFS 上无法跨机共享，
// 所以启动时必须自动把它转回 rollback journal。
func TestEnsureRollbackJournal_ConvertsLegacyWALDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sys_backend.db")
	newWALDatabase(t, path)

	rawConn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	assert.Equal(t, "wal", journalModeOf(t, rawConn), "去掉 journal_mode pragma 并不会把已有库转回 rollback journal")
	assert.Equal(t, byte(sqliteWALFormatVersion), mustFormatVersion(t, path))
	closeTestConn(t, rawConn)

	require.NoError(t, ensureRollbackJournal(path))
	assert.NotEqual(t, byte(sqliteWALFormatVersion), mustFormatVersion(t, path))

	// 转换必须把 WAL 中的数据 checkpoint 进主库，并且可以重复执行
	conn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	assert.Equal(t, "delete", journalModeOf(t, conn))
	var count int64
	require.NoError(t, conn.Raw("SELECT COUNT(*) FROM t").Scan(&count).Error)
	assert.Equal(t, int64(1), count, "转换不能丢数据")
	closeTestConn(t, conn)

	assert.NoError(t, ensureRollbackJournal(path))
}

// TestEnsureRollbackJournal_ResolvesDSN DSN 里的 file: URI 与查询串都必须先被解析成真实文件路径，
// 否则库头检查会被绕过（fail open）。
func TestEnsureRollbackJournal_ResolvesDSN(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
	}{
		{"普通路径带查询串", "%s?cache=shared"},
		{"file URI", "file:%s"},
		{"file URI 带查询串", "file:%s?cache=shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sys_backend.db")
			newWALDatabase(t, path)

			dsn := fmt.Sprintf(tc.format, filepath.ToSlash(path))
			require.NoError(t, ensureRollbackJournal(dsn), "DSN %q", dsn)
			assert.NotEqual(t, byte(sqliteWALFormatVersion), mustFormatVersion(t, path), "DSN %q 未能定位到库", dsn)
		})
	}
}

func TestSQLiteFilePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/mnt/udisk1/astra/astra_schedule.db", "/mnt/udisk1/astra/astra_schedule.db"},
		{"/mnt/udisk1/astra/astra_schedule.db?cache=shared", "/mnt/udisk1/astra/astra_schedule.db"},
		{"file:/mnt/udisk1/astra/astra_schedule.db", "/mnt/udisk1/astra/astra_schedule.db"},
		{"file:/mnt/udisk1/astra/astra_schedule.db?cache=shared", "/mnt/udisk1/astra/astra_schedule.db"},
		{"file:///mnt/udisk1/astra/astra_schedule.db", "/mnt/udisk1/astra/astra_schedule.db"},
		{"file://localhost/mnt/udisk1/astra/astra_schedule.db", "/mnt/udisk1/astra/astra_schedule.db"},
		{"file:/mnt/udisk1/astra/my%20schedule.db", "/mnt/udisk1/astra/my schedule.db"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sqliteFilePath(c.in), "DSN %q", c.in)
	}
}

func TestSQLiteDSN_AppendsBusyTimeout(t *testing.T) {
	assert.Equal(t, "data/sys_backend.db?_pragma=busy_timeout(5000)",
		sqliteDSN("data/sys_backend.db"))
	assert.Equal(t, "file:/data/sys_backend.db?_pragma=busy_timeout(5000)",
		sqliteDSN("file:/data/sys_backend.db"))

	dsn := sqliteDSN("file:/data/sys_backend.db?mode=ro")
	assert.Equal(t, "file:/data/sys_backend.db?mode=ro&_pragma=busy_timeout(5000)", dsn)
	assert.Equal(t, 1, strings.Count(dsn, "?"), "按 URI 规则追加，不能出现第二个 ?")
	assert.NotContains(t, dsn, "journal_mode", "运行期不得做 journal 模式转换")
}

// TestSQLiteDSN_KeepsRollbackJournal 新库用本项目的 DSN 打开后必须仍是 rollback journal
func TestSQLiteDSN_KeepsRollbackJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	conn, err := gorm.Open(gormsqlite.Open(sqliteDSN(path)))
	require.NoError(t, err)
	require.NoError(t, conn.Exec("CREATE TABLE t (a integer)").Error)

	assert.Equal(t, "delete", journalModeOf(t, conn))
	closeTestConn(t, conn)
}

func TestConnectSysDBCreatesDatabaseForFileURI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "sys_backend.db")
	_, err := os.Stat(filepath.Dir(path))
	require.ErrorIs(t, err, os.ErrNotExist)

	originalConfigs := config.Configs
	originalSysDB := SysDB
	config.Configs.SysDB = config.DBConfig{
		Type: "sqlite",
		Path: "file:" + filepath.ToSlash(path),
	}
	t.Cleanup(func() {
		if SysDB != nil && SysDB != originalSysDB {
			closeTestConn(t, SysDB)
		}
		SysDB = originalSysDB
		config.Configs = originalConfigs
	})

	ConnectSysDB()
	_, err = os.Stat(path)
	require.NoError(t, err)
}
