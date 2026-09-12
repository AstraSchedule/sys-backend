package db

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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

	// sqliteHeaderSize SQLite 库头长度。
	sqliteHeaderSize = 100
	// sqliteHeaderMagic 库头魔数（前 16 字节）。
	sqliteHeaderMagic = "SQLite format 3\x00"
	// sqliteFormatVersionOffset 库头中“文件格式读写版本”的偏移量。
	sqliteFormatVersionOffset = 18
	// sqliteWALFormatVersion 文件格式版本取该值表示 WAL。
	sqliteWALFormatVersion = 2
)

// sqliteDSN 构造 SQLite DSN，显式指定忙等待超时。
// 驱动是纯 Go 的 modernc.org/sqlite（由 libtnb/sqlite 封装）：只识别 _pragma/_txlock 等键，
// _busy_timeout 这类键会被静默忽略，所以超时必须写成 _pragma=busy_timeout(...)。
// 这里刻意不写 journal_mode：库在 NFS 上被多台机器共享，绝不能在运行期做 WAL→rollback journal
// 的转换（那需要一次 checkpoint，而本机的 -shm 视图与其他机器并不一致），转换只能离线做，
// 见 checkNotWAL。
func sqliteDSN(path string) string {
	return fmt.Sprintf("%s?_pragma=busy_timeout(%d)", path, sqliteBusyTimeout)
}

// checkNotWAL 在连接之前直接读库头，拒绝打开 WAL 模式的库。
// 本服务与 usr-backend 分处两台机器、经 NFS 打开同一个库，而 WAL 的 -shm 共享内存在 NFS 客户端
// 之间并不共享，跨机打开 WAL 库会造成 "database disk image is malformed"。
// journal_mode 是持久化在库头里的属性：库里一旦被写成 WAL，之后即使不带任何 pragma 打开也仍然是
// WAL，所以必须显式转换回 rollback journal 才能跨机共享。这里不做运行期转换，只拒绝并给出
// 离线转换命令——转换过程本身就可能造成二次损坏。
func checkNotWAL(path string) error {
	if strings.HasPrefix(path, ":memory:") {
		return nil // 内存库不跨进程共享
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // 新库，由 SQLite 创建为 rollback journal 模式
		}
		return fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, sqliteHeaderSize)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("读取 SQLite 数据库头失败: %w", err)
	}
	if n < sqliteHeaderSize || string(header[:len(sqliteHeaderMagic)]) != sqliteHeaderMagic {
		return nil // 不是已初始化的 SQLite 库，交给驱动自己报错
	}

	if header[sqliteFormatVersionOffset] == sqliteWALFormatVersion {
		return fmt.Errorf("SQLite 数据库 %s 处于 WAL 模式：WAL 依赖 -shm 共享内存，无法在 NFS 上跨机共享，"+
			"继续使用会造成 \"database disk image is malformed\"。请先停止所有后端，再执行 "+
			"sqlite3 %s \"PRAGMA journal_mode=%s;\" 转换后再启动", path, path, sqliteJournalMode)
	}
	return nil
}
