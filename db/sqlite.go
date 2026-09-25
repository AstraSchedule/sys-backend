package db

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	gormsqlite "github.com/libtnb/sqlite"
	"github.com/sirupsen/logrus"
)

const (
	// sqliteBusyTimeout SQLite 忙等待超时（毫秒）。Astra 库由 usr-backend（函数计算实例）经 NFS
	// 同时打开，锁冲突时必须等待重试，而不是立刻返回 SQLITE_BUSY。
	sqliteBusyTimeout = 5000

	// sqliteJournalMode 唯一允许的 journal 模式。
	// WAL 依赖 -shm 共享内存来协调读写，而 NFS 客户端之间并不共享这份内存：同一个库被两台机器
	// 同时打开时，双方看到的是两套互不可见的 WAL 索引，checkpoint 与读取会对不上，最终报
	// "database disk image is malformed"。因此共享的 SQLite 库只允许 rollback journal。
	sqliteJournalMode = "DELETE"

	// sqliteConvertAttempts 启动时把 WAL 库转回 rollback journal 的尝试次数。
	// 转换需要独占锁并做一次 checkpoint，两端同时启动时会互相竞争，所以这里带重试。
	sqliteConvertAttempts = 3
	// sqliteConvertRetryDelay 每次重试前的等待时间（按次数递增）
	sqliteConvertRetryDelay = 300 * time.Millisecond

	// sqliteHeaderSize SQLite 库头长度。
	sqliteHeaderSize = 100
	// sqliteHeaderMagic 库头魔数（前 16 字节）。
	sqliteHeaderMagic = "SQLite format 3\x00"
	// sqliteFormatVersionOffset 库头中“文件格式读写版本”的偏移量。
	sqliteFormatVersionOffset = 18
	// sqliteWALFormatVersion 文件格式版本取该值表示 WAL。
	sqliteWALFormatVersion = 2
)

// sqliteDSN 在 DSN 上追加忙等待超时，并剔除会推翻 journal 模式的 pragma。
// 驱动是纯 Go 的 modernc.org/sqlite（由 libtnb/sqlite 封装）：只识别 _pragma/_txlock 等键，
// _busy_timeout 这类键会被静默忽略，所以超时必须写成 _pragma=busy_timeout(...)。
// DSN 可能已经带查询串（file: URI），因此按 URI 规则用 & 追加，不能无条件拼 ?。
//
// DSN 里若写了 _pragma=journal_mode(WAL)，正式连接会把 ensureRollbackJournal 刚转好的库
// 重新推回 WAL —— 而 WAL 依赖的 -shm 在 NFS 客户端之间不共享，跨机使用会损坏数据库。
// 这里直接剔除这类 pragma 并留 WARN：让配置错误可见，但绝不让它悄悄生效。
func sqliteDSN(dsn string) string {
	base, query := splitSQLiteQuery(dsn)

	kept := make([]string, 0, 4)
	for _, param := range strings.Split(query, "&") {
		if param == "" {
			continue
		}
		if isJournalModePragmaParam(param) {
			logrus.Warnf("SQLite DSN %q 里的 %s 被忽略：journal 模式由启动时的转换统一管理，"+
				"WAL 在 NFS 上跨机共享会损坏数据库", dsn, param)
			continue
		}
		kept = append(kept, param)
	}
	kept = append(kept, fmt.Sprintf("_pragma=busy_timeout(%d)", sqliteBusyTimeout))

	return base + "?" + strings.Join(kept, "&")
}

// splitSQLiteQuery 把 DSN 拆成基础部分与查询串。
func splitSQLiteQuery(dsn string) (string, string) {
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		return dsn[:i], dsn[i+1:]
	}
	return dsn, ""
}

// isJournalModePragmaParam 判断一个查询串参数是否是 journal_mode pragma。
//
// 先做一次百分号解码再判断，否则 _pragma=journal%5Fmode(WAL) 这类写法能绕过剔除。
func isJournalModePragmaParam(param string) bool {
	key, value, found := strings.Cut(param, "=")
	if !found || key != "_pragma" {
		return false
	}
	if decoded, err := url.QueryUnescape(value); err == nil {
		value = decoded
	}
	return strings.Contains(strings.ToLower(value), "journal_mode")
}

// sqliteIsMemoryDSN 报告 DSN 指向内存库。
//
// 内存库不落盘、也不跨进程共享：既不需要检查 journal 模式，更不能照着路径部分去检查磁盘上的
// 同名文件 —— file:memdb?mode=memory&cache=shared 的路径部分是 memdb，照它去检查会查到一个
// 与本次连接毫无关系的磁盘文件，而转换用的却是原始 URI（真正打开的是内存库），
// 检查对象和转换对象根本不是同一个。
func sqliteIsMemoryDSN(dsn string) bool {
	if strings.HasPrefix(dsn, ":memory:") {
		return true
	}
	if !strings.HasPrefix(dsn, "file:") {
		return false
	}
	query := ""
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		query = dsn[i+1:]
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return false
	}
	return strings.EqualFold(values.Get("mode"), "memory")
}

// sqliteFilePath 从 DSN 中解析出磁盘上的库文件路径，兼容 file: URI 写法。
// 不带 file: 前缀的值按普通路径处理（Windows 路径不会被当成 URI scheme 解析）。
func sqliteFilePath(dsn string) string {
	if !strings.HasPrefix(dsn, "file:") {
		return trimSQLiteQuery(dsn)
	}

	path := trimSQLiteQuery(strings.TrimPrefix(dsn, "file:"))
	path = strings.TrimPrefix(path, "//localhost")
	path = strings.TrimPrefix(path, "//")
	if unescaped, err := url.PathUnescape(path); err == nil {
		return unescaped
	}
	return path
}

// trimSQLiteQuery 去掉 DSN 中的查询串
func trimSQLiteQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
}

// sqliteFormatVersion 读取库头里的文件格式版本（第 18 字节），取 sqliteWALFormatVersion 表示 WAL。
// 文件不存在（尚未创建）或尚未初始化时返回 0。
func sqliteFormatVersion(path string) (byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil // 新库，由 SQLite 创建为 rollback journal 模式
		}
		return 0, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, sqliteHeaderSize)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("读取 SQLite 数据库头失败: %w", err)
	}
	if n < sqliteHeaderSize || string(header[:len(sqliteHeaderMagic)]) != sqliteHeaderMagic {
		return 0, nil // 不是已初始化的 SQLite 库，交给驱动自己报错
	}
	return header[sqliteFormatVersionOffset], nil
}

// sqliteWALMessage 描述 WAL 在跨机 NFS 场景下的危害
func sqliteWALMessage(path string) string {
	return fmt.Sprintf("SQLite 数据库 %s 处于 WAL 模式：WAL 依赖 -shm 共享内存，无法在 NFS 上跨机共享，"+
		"继续使用会造成 \"database disk image is malformed\"", path)
}

// ensureRollbackJournal 确保库不是 WAL 模式：是 WAL 就显式转回 rollback journal。
//
// journal_mode 是持久化在库头里的属性，只从 DSN 里删掉 journal_mode(WAL) 并不会把存量库转回来；
// 而 WAL 依赖的 -shm 共享内存在 NFS 客户端之间并不共享，跨机同时使用会导致数据库损坏，
// 所以这一步必须在连接（以及 AutoMigrate）之前做掉。
//
// 转换需要独占锁并做一次 checkpoint：两端可能同时启动，因此带重试；
// 始终失败时返回错误（拒绝启动），由运维在所有后端停机后离线转换。
func ensureRollbackJournal(dsn string) error {
	if sqliteIsMemoryDSN(dsn) {
		return nil // 内存库不落盘、不跨进程共享，没有 journal 模式可转换
	}

	path := sqliteFilePath(dsn)
	if path == "" {
		return fmt.Errorf("无法从 SQLite DSN %q 解析出库文件路径", dsn)
	}

	var lastErr error
	for attempt := 1; attempt <= sqliteConvertAttempts; attempt++ {
		version, err := sqliteFormatVersion(path)
		if err != nil {
			return err
		}
		if version != sqliteWALFormatVersion {
			return nil // 已经是 rollback journal（也可能是另一端刚转换完）
		}

		if attempt == 1 {
			logrus.Warnf("%s，正在转换为 %s 模式", sqliteWALMessage(path), sqliteJournalMode)
		}
		if lastErr = convertToRollbackJournal(dsn); lastErr == nil {
			logrus.Infof("SQLite 数据库 %s 已转换为 %s 模式", path, sqliteJournalMode)
			return nil
		}
		logrus.Warnf("转换 SQLite journal 模式失败（第 %d 次）: %v", attempt, lastErr)
		if attempt < sqliteConvertAttempts {
			time.Sleep(time.Duration(attempt) * sqliteConvertRetryDelay)
		}
	}

	return fmt.Errorf("%s；自动转换失败: %v。请先停止所有后端，再执行 "+
		"sqlite3 %s \"PRAGMA journal_mode=%s;\" 转换后再启动",
		sqliteWALMessage(path), lastErr, path, sqliteJournalMode)
}

// convertToRollbackJournal 用一个短连接执行一次 journal 模式转换
func convertToRollbackJournal(dsn string) error {
	conn, err := sql.Open(gormsqlite.DriverName, sqliteDSN(dsn))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var mode string
	if err := conn.QueryRow("PRAGMA journal_mode=" + sqliteJournalMode).Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, sqliteJournalMode) {
		return fmt.Errorf("journal_mode 仍为 %s", mode)
	}
	return nil
}
