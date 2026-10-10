// Command cleanup-orphan-notifications 清理指向不存在（或已软删）单据的站内通知。
//
// 适用场景：
//   - 单据被绕过应用的硬删/清理（如手工 SQL、测试夹具）后，通知中心残留死链；
//   - 历史脏数据/存量孤儿通知的一次性巡检与清理。
//
// 默认 dry-run（只打印不删除）；确认无误后加 -apply 执行删除。
// 产品内删除路径已自动联动清理，本命令是兜底工具。
//
// 用法示例（在 itsm-backend 目录下，读取 .env 的 DB_* 配置）：
//
//	go run ./cmd/cleanup-orphan-notifications                 # 全租户 dry-run
//	go run ./cmd/cleanup-orphan-notifications -tenant 11      # 指定租户
//	go run ./cmd/cleanup-orphan-notifications -apply          # 执行删除
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"itsm-backend/ent"
	"itsm-backend/service"

	_ "github.com/lib/pq"
)

func loadEnv(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) == 2 {
			m[strings.TrimSpace(kv[0])] = strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		}
	}
	return m
}

func main() {
	apply := flag.Bool("apply", false, "执行删除（默认 dry-run，仅打印）")
	tenantID := flag.Int("tenant", 0, "仅处理指定租户（0=全部租户）")
	limit := flag.Int("limit", 10000, "单次扫描的最大通知行数（0=不限制）")
	flag.Parse()

	env := loadEnv(".env")
	dsn := env["DATABASE_URL"]
	if dsn == "" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
			env["DB_USER"], env["DB_PASSWORD"], env["DB_HOST"], env["DB_PORT"], env["DB_NAME"], env["DB_SSLMODE"])
	}
	client, err := ent.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if *apply {
		orphans, err := service.CleanupOrphanResourceNotifications(ctx, client, *tenantID, *limit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cleanup:", err)
			os.Exit(1)
		}
		printOrphans(orphans, true)
		fmt.Printf("已删除孤儿通知 %d 条。\n", len(orphans))
		return
	}

	orphans, err := service.ScanOrphanResourceNotifications(ctx, client, *tenantID, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}
	printOrphans(orphans, false)
	fmt.Printf("发现孤儿通知 %d 条（dry-run，未删除；加 -apply 执行）。\n", len(orphans))
}

func printOrphans(orphans []service.OrphanNotification, deleted bool) {
	if len(orphans) == 0 {
		fmt.Println("未发现孤儿通知。")
		return
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].ID < orphans[j].ID })
	action := "待清理"
	if deleted {
		action = "已清理"
	}
	fmt.Printf("%-8s %-8s %-16s %-10s %-8s %s\n", "通知ID", "租户ID", "资源类型", "资源ID", "状态", "action_url")
	for _, o := range orphans {
		fmt.Printf("%-8d %-8d %-16s %-10d %-8s %s\n",
			o.ID, o.TenantID, string(o.ResourceType), o.ResourceID, action, o.ActionURL)
	}
}
