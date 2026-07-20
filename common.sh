# 网站备份目录
BACKUP_STORAGE_DIR=$DNMP_DIR/website/backups
# 虚拟机根目录
VHOSTS_ROOT=$DNMP_DIR/website/wwwroot
# 虚拟机配置目录
VHOSTS_CONF_DIR=$DNMP_DIR/website/http.d
# MySQL数据目录
MYSQL_DATA_DIR=$DNMP_DIR/website/data
# 日志目录
LOGS_ROOT=$DNMP_DIR/website/logs
# 初始化运行目录。逐项创建可兼容不完整恢复和首次部署。
mkdir -p "$BACKUP_STORAGE_DIR"
mkdir -p "$VHOSTS_ROOT"
mkdir -p "$VHOSTS_CONF_DIR"
mkdir -p "$MYSQL_DATA_DIR"
mkdir -p "$LOGS_ROOT"/{nginx,php83,certbot}

# RC红色错误  GC绿色成功  LG浅绿输出  BC蓝色输入  SB天蓝确认  CC橙色提示  PC粉色强调
RC="\033[38;5;196m"; RR="\033[31m"; GC="\033[38;5;82m"; LG="\033[38;5;72m"; BC="\033[39;1;34m"; SB="\033[38;5;45m"
CC="\033[38;5;208m"; PC="\033[38;5;201m"; YC="\033[38;5;148m"; ED="\033[0m";

# 红色错误
function echoRC {
    echo -e "$RC${1}$ED"
}
# 玫红
function echoRR {
    echo -e "$RR${1}$ED"
}
# 绿色成功
function echoGC {
    echo -e "$GC${1}$ED"
}
# 浅绿输出
function echoLG {
    echo -e "$LG${1}$ED"
}
# 天蓝
function echoBC {
    echo -e "$BC${1}$ED"
}
# SB
function echoSB {
    echo -e "$SB${1}$ED"
}
# 橙色提示
function echoCC {
    echo -e "$CC${1}$ED"
}
# PC粉色
function echoPC {
    echo -e "$PC${1}$ED"
}
# 黄色
function echoYC {
    echo -e "$YC${1}$ED"
}
