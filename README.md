# hxoa

一个给 hark 使用的本地工作区映射客户端.

hxoa 把本地工作区映射给 hark, 让它能像读本地文件一样查阅工作区.
它是一个零信任设计的只读客户端, 只做一件事, 把工作区安全地映射到会话里供查阅.

## 为什么是零信任

- 源码完全公开, 你可以自己 clone 下来逐行核对再编译, 不需要相信任何预编译产物.
- 客户端本身不持有任何工作区内容, 也不缓存文件, 每次查看都是实时映射.
- 连接需要一个一次性令牌, 令牌只绑定当前会话, 换一个会话就要重新连接.
- 只读设计, 客户端没有任何执行命令或改写工作区的能力. 笔记只写在你本机的客户端目录, 不经过网络.
- 域名和令牌都可以配置, 不写死在任何地方, 换一个环境就能重新指过去.

## 从源码构建

你不需要下载任何二进制, 自己编译最稳妥. 需要 Go 1.22 或更新版本.

```bash
git clone https://github.com/HengXin666/hxoa.git
cd hark-workspace-bridge
go build -o hxoa .
./hxoa --help
```

编译完会得到一个 hxoa 可执行文件. 交叉编译其他架构:

```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o hxoa-linux-arm64 .
```

## 用法

```bash
hxoa login <token> --base <url>   连接工作区, 也可以设 HARK_BASE 环境变量
hxoa ls [path]                    列出目录
hxoa cat <path>                   打印文件
hxoa git <command>                只读 git 查询 (status/log/show/diff 等)
hxoa root                         打印工作区根目录
hxoa note <file>                  把笔记存到本机客户端目录
hxoa note <file>                  把笔记存到本机客户端目录
```

路径都相对于工作区根目录.

## 笔记与源码分离

改代码和记笔记是两件事. hxoa 把笔记单独存成一个本地文件, 和源码分开,
方便回头查阅当时的思路, 也不会混进代码目录. 记笔记就是存一个本地文件, 只是换了个更顺手的位置.

## 构建流水线

源码每次提交由 Actions 自动编译 linux amd64 与 arm64 并放到 Releases. 但你不依赖它, 自己 build 即可.
