// Package dashboard embeds web assets at compile time (//go:embed), so `forge dashboard`
// can serve the dashboard from a single binary without distributing static files. Isomorphic with skills/embed: embed auto-excludes
// .go files carrying build directives (embed.go itself); pulse.html under assets/ is the only asset.
//
// Package dashboard 内嵌 web 资产（编译期 //go:embed），使 `forge dashboard`
// 单二进制即可起看板，无需随包分发静态文件。与 skills/embed 同构：embed 自动排除
// 含 build 指令的 .go（embed.go 自身），assets/ 下的 pulse.html 是唯一资产。
package dashboard

import "embed"

// assetsFS is the compile-time-embedded dashboard asset virtual filesystem (paths relative to this package, slash-separated).
//
// assetsFS 是编译期嵌入的看板资产虚拟文件系统（路径相对本包，正斜杠分隔）。
//
//go:embed assets
var assetsFS embed.FS
