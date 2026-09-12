package db

import (
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

func TestCheckNotWAL_AllowsNewDatabase(t *testing.T) {
	assert.NoError(t, checkNotWAL(filepath.Join(t.TempDir(), "new.db")))
	assert.NoError(t, checkNotWAL(":memory:"))
}

// TestCheckNotWAL_RejectsWALDatabase 复现线上事故：库一旦被写成 WAL，之后不带任何 pragma 打开
// 也仍然是 WAL，跨机（NFS）使用就会损坏，所以必须在连接前拦住并提示离线转换。
func TestCheckNotWAL_RejectsWALDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "astra_schedule.db")
	newWALDatabase(t, path)

	rawConn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	var mode string
	require.NoError(t, rawConn.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	assert.Equal(t, "wal", mode, "去掉 journal_mode pragma 并不会把已有库转回 rollback journal")
	closeTestConn(t, rawConn)

	err = checkNotWAL(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WAL 模式")
	assert.Contains(t, err.Error(), "journal_mode="+sqliteJournalMode)

	// 离线转换之后必须放行
	convConn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	require.NoError(t, convConn.Exec("PRAGMA journal_mode="+sqliteJournalMode).Error)
	closeTestConn(t, convConn)
	assert.NoError(t, checkNotWAL(path))
}

// TestCheckNotWAL_RejectsWALDatabaseWithDSNSuffix DSN 里的 file: URI 与查询串都必须先被解析成
// 真实文件路径，否则库头检查会被绕过（fail open）。
func TestCheckNotWAL_RejectsWALDatabaseWithDSNSuffix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "astra_schedule.db")
	newWALDatabase(t, path)
	slashed := filepath.ToSlash(path)

	for _, dsn := range []string{
		slashed + "?cache=shared",
		"file:" + slashed,
		"file:" + slashed + "?cache=shared",
	} {
		err := checkNotWAL(dsn)
		require.Error(t, err, "DSN %q 必须能定位到 WAL 库", dsn)
		assert.Contains(t, err.Error(), "WAL 模式")
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

	var mode string
	require.NoError(t, conn.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	assert.Equal(t, "delete", mode)
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
