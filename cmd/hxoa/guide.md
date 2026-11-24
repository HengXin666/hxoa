hxoa: 连接用户本机工作区的客户端. 路径都相对于工作区根目录.
元信息 (version 等) 以 "# ..." 打到 stderr, 内容打到 stdout.
退出码: 0 成功, 1 错误, 2 用法错误, 3 冲突, 4 不存在, 其他=命令自身退出码.

连接
  hxoa login <token> --base <url>   连接 (令牌即身份, 由用户在面板上创建)
  HXOA_PROFILE=<名字> hxoa ...       同时连多个工作区时, 每个用一个 profile
  hxoa status                       看工作区, 身份, 模式 (direct / worker)
  hxoa logout                       断开

查看 (省 token: 先 ls/grep 定位, 再按行读)
  hxoa ls [path] [-d 深度] [-m 上限]     目录; 默认 1 层, 跳过 .git node_modules
  hxoa grep <pattern> [path] [-g '*.go'] [-F] [-i] [-m 200]
  hxoa cat <path> [start] [end] [-n]   读文件, 可按行; -n 带行号
                                     # path v=<version> lines=N  (按行读时还有 rv=<range_version>)
  hxoa stat <path>                     只取 version
  hxoa pull <path> <本地目录>           打包下载整个目录 (本地编译/分析用)

修改 (都基于你读取时的版本, 期间文件被任何人改过就返回冲突, 不会覆盖)
  客户端会记住你最后一次 cat/stat/写入 某个文件时的版本, 所以 -v 一般不用写;
  没读过的文件不能改. 冲突后记忆会清掉, 必须重新 cat.
  hxoa replace <path> --old '原文' --new '新文'      精确替换, 原文必须唯一 (--all 全部)
  hxoa replace <path> --old-file a --new-file b     原文/新文放本地文件里 (多行最稳)
  hxoa replace <path> < edits.json                  多处: [{"old":..,"new":..,"all":false}]
  hxoa patch <path> <start> <end> < 新行             替换第 start..end 行 (1 起, 含两端)
  hxoa patch <path> <start> <end> -r <rv> < 新行     只校验这几行 (按行读取给的 rv), 文件别处被改也能写
                                                    插入: end=start-1; 删除: stdin 为空
  hxoa write <path> --new < 内容                     新建 (已存在会拒绝)
  hxoa write <path> < 内容                           整体改写
  hxoa push <本地文件> <path> [--new]                 上传本地文件
  hxoa rm <path>     hxoa mv <from> <to>
  -v <version> 可显式指定版本.
  --force 跳过版本检查, 只在用户明确要求时用.

命令
  hxoa exec <command>                在根目录用 bash -lc 执行; 单个参数原样传, 管道可用
      --timeout 秒 (默认 120, 最长 1800)  --dir 子目录  --stdin (把本地 stdin 传过去)
      --bg 后台跑, 返回 job id
  hxoa job <id> [--wait 60] [--kill]  看后台任务输出 / 等待 / 终止
  hxoa git <args...>                  等于 exec git ...
  输出超过 64K 时保留头尾.

规则
  1. 改文件前先读; 冲突 (退出码 3) 就重新读, 在新内容上重做修改, 不要 --force.
  2. 改动小就用 replace (不依赖行号, 最不容易改错) 或按行 patch, 不要整文件 write.
  3. 按行 patch 之后行号会变; 连续改同一文件的多处, 优先 replace, 或从下往上改.
  4. 不要凭记忆改: 很久没看过的地方先 cat 那几行再改.
  5. worker 模式: 你在自己的 git 工作树和分支里, 用户的目录不会变.
     做完一段就 hxoa git add -A, hxoa git commit -m '...', 再告诉用户去面板合并.
     要用户的最新代码: hxoa git merge <基础分支>; 面板报合并冲突时也这样在你的分支里先解决.
  6. direct 模式: 你直接改用户的文件, 用户和本地工具可能同时在改, 尊重冲突.
  7. 不要动 .git 和 .hxoa (服务端也会拒绝写入).
