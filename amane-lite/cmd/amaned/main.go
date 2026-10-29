// amaned 是精简版 Amane 服务的入口：只做刮削、扫描与整理。
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fushuqian/amane-lite/internal/api"
	"github.com/fushuqian/amane-lite/internal/config"
	"github.com/fushuqian/amane-lite/internal/fetch"
	"github.com/fushuqian/amane-lite/internal/store"
	"github.com/fushuqian/amane-lite/internal/worker"
)

func main() {
	dataDir := flag.String("data", env("AMANE_DATA_DIR", env("MICA_MODULE_DATA_DIR", "./data")), "数据目录")
	addr := flag.String("addr", env("AMANE_ADDR", ""), "监听地址（tcp:// 或 unix://）")
	media := flag.String("media", env("AMANE_MEDIA_DIR", ""), "片库根目录")
	downloads := flag.String("downloads", env("AMANE_DOWNLOAD_DIR", ""), "下载库根目录")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	cfgPath := filepath.Join(*dataDir, "config.json")
	manager, err := config.NewManager(cfgPath, *media, *downloads)
	if err != nil {
		log.Fatalf("载入配置失败: %v", err)
	}

	listen := *addr
	if sidecar := os.Getenv("MICA_SIDECAR_ADDR"); sidecar != "" {
		// Mica Home 会把 sidecar 的监听地址注入进来：tcp://127.0.0.1:<port> 或 unix:///...
		listen = sidecar
	}
	if listen == "" {
		listen = manager.Get().Addr
	}

	st, err := store.Open(store.DatabasePath(*dataDir))
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()
	if err := st.ResetRunning(); err != nil {
		log.Fatalf("重置任务状态失败: %v", err)
	}

	client, err := fetch.New(fetch.Config{ProxyURL: manager.Get().EffectiveProxy()})
	if err != nil {
		log.Fatalf("创建 HTTP 客户端失败: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	w := worker.New(st, client, manager.Get)
	go w.Start(ctx)

	server := &api.Server{St: st, Client: client, Cfg: manager}
	httpServer := &http.Server{
		Handler:           api.TokenGuard(server.Handler()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := listenNetwork(listen)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}
	log.Printf("amaned %s 监听 %s", api.Version, listen)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("服务退出: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	_ = httpServer.Shutdown(shutdownCtx)
	log.Println("已停止")
}

// listenNetwork 支持 tcp://host:port 与 unix:///path 两种形态。
func listenNetwork(address string) (net.Listener, error) {
	switch {
	case strings.HasPrefix(address, "unix://"):
		path := strings.TrimPrefix(address, "unix://")
		_ = os.Remove(path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		return net.Listen("unix", path)
	case strings.HasPrefix(address, "tcp://"):
		return net.Listen("tcp", strings.TrimPrefix(address, "tcp://"))
	default:
		return net.Listen("tcp", address)
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
