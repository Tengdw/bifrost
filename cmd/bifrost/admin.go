package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"

	"bifrost/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"
)

func runAdmin(ctx context.Context, pool *pgxpool.Pool, recover bool) error {
	exists, err := auth.AdminExists(ctx, pool)
	if err != nil {
		return errors.New("无法读取管理员状态，请先迁移数据库")
	}
	if exists && !recover {
		return nil
	}
	if !exists && recover {
		return errors.New("管理员尚未初始化，请使用 -admin-init")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return errors.New("请在交互终端执行管理员命令")
	}
	defer tty.Close()
	if recover {
		fmt.Fprintln(tty, "恢复管理员将撤销所有旧会话与 Passkey。")
	}
	fmt.Fprint(tty, "管理员用户名：")
	name, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil {
		return errors.New("无法读取管理员用户名")
	}
	password, err := promptPassword(tty, "设置密码（至少 12 字节）：")
	if err != nil {
		return err
	}
	confirmation, err := promptPassword(tty, "再次输入密码：")
	if err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("两次密码输入不一致")
	}
	if err := auth.SetAdmin(ctx, pool, strings.TrimSpace(name), password, recover); err != nil {
		return err
	}
	fmt.Fprintln(tty, "管理员已设置。")
	return nil
}

func promptPassword(tty *os.File, label string) (string, error) {
	fmt.Fprint(tty, label)
	value, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	return string(value), err
}

func runDemoAdmin(ctx context.Context, pool *pgxpool.Pool, mode string) error {
	if mode != "synthetic" && mode != "snapshot" {
		return errors.New("-demo-admin 只能用于合成演示")
	}
	if mode == "synthetic" {
		var stored string
		if err := pool.QueryRow(ctx, "select mode from bifrost.workspace_config where singleton").Scan(&stored); err != nil || stored != mode {
			return errors.New("请先初始化隔离的合成演示数据库")
		}
	} else {
		var workspaceExists bool
		if err := pool.QueryRow(ctx, "select exists(select 1 from bifrost.workspace_config)").Scan(&workspaceExists); err != nil || workspaceExists {
			return errors.New("请先初始化隔离的快照演示数据库")
		}
	}
	exists, err := auth.AdminExists(ctx, pool)
	if err != nil {
		return errors.New("演示库尚未迁移")
	}
	if exists {
		return errors.New("演示管理员已存在，请使用新的隔离演示数据库")
	}
	password := rand.Text()
	if err := auth.SetAdmin(ctx, pool, "demo", password, false); err != nil {
		return err
	}
	fmt.Println(password)
	return nil
}
