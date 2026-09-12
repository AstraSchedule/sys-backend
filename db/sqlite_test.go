package db

import (
	"path/filepath"
	"testing"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func TestSQLiteDSN_SetsBusyTimeout(t *testing.T) {
	dsn := sqliteDSN(filepath.Join("data", "sys_backend.db"))
	assert.Contains(t, dsn, "_pragma=busy_timeout(5000)")
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
