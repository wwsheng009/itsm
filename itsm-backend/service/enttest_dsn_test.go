package service

import (
	"fmt"
	"sync/atomic"
)

var testDBCounter int64

// testDSN 每次调用返回唯一的 SQLite 内存数据库 DSN。
// 历史上所有测试共用 "file:ent?mode=memory&cache=shared&_fk=1"，
// 当上一个测试的连接池尚未完全关闭时，下一个测试会连接到同一个残留数据库，
// 触发 users.email 等唯一约束的偶发冲突。唯一命名可彻底隔离测试间数据。
func testDSN() string {
	return uniqueTestDSN("svc_test")
}

// uniqueTestDSN 生成带进程内唯一后缀的内存库 DSN（复用 testDSN 的历史修复范式）。
// 供使用自定义前缀的测试夹具（如 biz_* 业务流）消除同名共享库的偶发唯一约束冲突。
func uniqueTestDSN(prefix string) string {
	return fmt.Sprintf("file:%s_%d?mode=memory&cache=shared&_fk=1", prefix, atomic.AddInt64(&testDBCounter, 1))
}
