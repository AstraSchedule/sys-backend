package db

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"sys-backend/config"
)

// walCrashWriteEnv 非空时，本测试二进制进入「崩溃写入」子进程模式。
const walCrashWriteEnv = "ASTRA_TEST_WAL_CRASH_WRITE"

// walCrashRows 子进程写入的行数。
const walCrashRows = 3

// TestMain 兼作 fixture：需要「未 checkpoint 的 WAL」时，父测试用 walCrashWriteEnv
// 启动本二进制，子进程写完直接退出（见 walCrashWrite）。
func TestMain(m *testing.M) {
	if path := os.Getenv(walCrashWriteEnv); path != "" {
		walCrashWrite(path)
		return
	}
	os.Exit(m.Run())
}

// walCrashWrite 把库写成 WAL、写入数据，然后不关连接直接退出。
//
// 关键在于「不 Close」：关闭最后一个连接会触发 checkpoint 并删掉 -wal 文件，
// 那样只存在于 WAL 中的已提交帧就没了，测试也就无法证明转换把它们保住了。
func walCrashWrite(path string) {
	conn, err := gorm.Open(gormsqlite.Open(path + "?_pragma=journal_mode(WAL)"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "崩溃写入: 打开库失败: %v\n", err)
		os.Exit(2)
	}
	if err := conn.Exec("CREATE TABLE t (a integer)").Error; err != nil {
		fmt.Fprintf(os.Stderr, "崩溃写入: 建表失败: %v\n", err)
		os.Exit(2)
	}
	for i := 1; i <= walCrashRows; i++ {
		if err := conn.Exec("INSERT INTO t VALUES (?)", i).Error; err != nil {
			fmt.Fprintf(os.Stderr, "崩溃写入: 插入失败: %v\n", err)
			os.Exit(2)
		}
	}
	os.Exit(0)
}

// spawnWALCrashWriter 起一个子进程，把 path 写成带未 checkpoint 数据的 WAL 库。
func spawnWALCrashWriter(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), walCrashWriteEnv+"="+path)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "WAL 崩溃写入子进程失败: %s", out)
}

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

// TestEnsureRollbackJournal_PreservesUncheckpointedWALFrames 转换必须把「只存在于 WAL 中的
// 已提交帧」checkpoint 进主库。
//
// 用子进程异常退出来制造这种状态：正常关闭最后一个连接会触发 checkpoint 并删掉 -wal，
// 那样的库根本考不出「转换到底有没有丢 WAL 里的数据」。
func TestEnsureRollbackJournal_PreservesUncheckpointedWALFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "astra.db")
	spawnWALCrashWriter(t, path)

	walInfo, err := os.Stat(path + "-wal")
	require.NoError(t, err, "子进程异常退出后应留下 -wal 文件")
	require.Positive(t, walInfo.Size(), "-wal 里必须有未 checkpoint 的帧，否则这个用例证明不了什么")

	require.NoError(t, ensureRollbackJournal(path))
	require.NoFileExists(t, path+"-wal", "转换后 WAL 应已 checkpoint 并清理")

	conn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	defer closeTestConn(t, conn)

	assert.Equal(t, "delete", journalModeOf(t, conn))
	var count int64
	require.NoError(t, conn.Raw("SELECT COUNT(*) FROM t").Scan(&count).Error)
	assert.Equal(t, int64(walCrashRows), count, "只存在于 WAL 中的已提交帧不能在转换时丢失")
}

// TestEnsureRollbackJournal_SkipsMemoryDSN 内存库既不该被检查也不该被转换：
// file:memdb?mode=memory 的路径部分是 memdb，照它去找磁盘会碰到一个与本次连接无关的同名文件。
func TestEnsureRollbackJournal_SkipsMemoryDSN(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// 在临时目录里放一个同名的 WAL 磁盘库：如果实现错误地去检查/转换它，这个文件会被改动。
	diskPath := filepath.Join(dir, "memdb")
	newWALDatabase(t, diskPath)

	require.NoError(t, ensureRollbackJournal("file:memdb?mode=memory&cache=shared"))
	assert.Equal(t, byte(sqliteWALFormatVersion), mustFormatVersion(t, diskPath),
		"内存库 DSN 不该动到磁盘上的同名文件")

	// :memory: 与带查询串的 file: URI 都算内存库
	assert.True(t, sqliteIsMemoryDSN(":memory:"))
	assert.True(t, sqliteIsMemoryDSN("file:memdb?mode=memory&cache=shared"))
	assert.True(t, sqliteIsMemoryDSN("file:memdb?cache=shared&mode=memory"))
	assert.False(t, sqliteIsMemoryDSN("file:/data/astra.db?cache=shared"))
	assert.False(t, sqliteIsMemoryDSN("/data/astra.db"))
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

// TestSQLiteDSN_StripsWALPragma DSN 里带 journal_mode(WAL) 时必须被剔除：
// 正式连接重新开启 WAL 会推翻启动时的转换，而 WAL 在 NFS 上跨机共享会损坏数据库。
func TestSQLiteDSN_StripsWALPragma(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	conn, err := gorm.Open(gormsqlite.Open(path))
	require.NoError(t, err)
	require.NoError(t, conn.Exec("CREATE TABLE t (a integer)").Error)
	closeTestConn(t, conn)

	dsn := sqliteDSN("file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&cache=shared")
	assert.NotContains(t, dsn, "journal_mode", "冲突的 pragma 必须被剔除，运行期不得做 journal 模式转换")
	assert.Contains(t, dsn, "cache=shared", "其余查询参数必须原样保留")

	formal, err := gorm.Open(gormsqlite.Open(dsn))
	require.NoError(t, err)
	defer closeTestConn(t, formal)
	assert.Equal(t, "delete", journalModeOf(t, formal), "正式连接不得被 DSN 里的 pragma 推回 WAL")
}

// TestSQLiteDSN_DropsEncodedJournalPragma 百分号编码的 pragma 同样要能被识别，
// 否则 journal%5Fmode 这类写法可以绕过剔除。
func TestSQLiteDSN_DropsEncodedJournalPragma(t *testing.T) {
	dsn := sqliteDSN("file:/data/astra.db?_pragma=journal%5Fmode(WAL)&mode=ro")
	assert.NotContains(t, strings.ToLower(dsn), "journal", "编码后的 pragma 也必须被剔除")
	assert.Contains(t, dsn, "mode=ro")
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
