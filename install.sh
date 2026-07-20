#!/bin/bash
set -Eeuo pipefail

# 获取当前脚本的软链接路径
SYMLINK_PATH=$(readlink -f "$0")
# DNMP目录
DNMP_DIR=$(dirname "$SYMLINK_PATH")
# 路径转换
DNMP_DIR=$(realpath "$DNMP_DIR")
# .env文件路径
DNMP_ENV="$DNMP_DIR/.env"
# 加载公共变量
source "$DNMP_DIR/common.sh"

# 检查是否为 root 用户
if [ "$EUID" -ne 0 ]; then
    echoRR "Please run this script with root privileges."
    exit 1
fi

# 检查.env文件是否存在 不存在则创建
if [ ! -f "$DNMP_ENV" ]; then
    cp "$DNMP_DIR/env.sample" "$DNMP_ENV"
fi

# 只在首次安装或密码为空时生成密码，重复执行不会破坏现有数据库。
if ! grep -Eq '^MYSQL_ROOT_PASSWORD=.+$' "$DNMP_ENV"; then
    MYSQL_ROOT_PASSWORD=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
    sed -i "s/^MYSQL_ROOT_PASSWORD=.*/MYSQL_ROOT_PASSWORD=$MYSQL_ROOT_PASSWORD/" "$DNMP_ENV"
fi
# 加载ENV
set -a
source "$DNMP_ENV"
set +a

# 更新源
echoSB "Update Source."
apt update
# 安装依赖包
echoSB "Install Necessary Packages."
apt install -y apt-transport-https ca-certificates curl gnupg lsb-release unzip gawk zstd pv bc tzdata cron openssl

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
    echoSB "Add Docker Official GPG Key and Repository."
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/$(lsb_release -is | tr '[:upper:]' '[:lower:]')/gpg" \
        | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/$(lsb_release -is | tr '[:upper:]' '[:lower:]') \
      $(lsb_release -cs) stable" > /etc/apt/sources.list.d/docker.list
    apt update -y
    echoSB "Install Docker Engine."
    apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
fi

# 判断是否安装成功 根据docker 命令是否存在
if [ -x "$(command -v docker)" ]; then
    systemctl start docker
    systemctl enable docker
    echoGC "Docker Install Success."
    echoSB "Start Docker Compose Service."
    cd "$DNMP_DIR"
    docker compose config --quiet
    docker compose pull
    docker compose up -d
else
    echoRR "Docker Install Failed."
    exit 1
fi

# 创建软链接
chmod +x "$DNMP_DIR/vhost.sh"
ln -sfn "$DNMP_DIR/vhost.sh" /usr/local/bin/vhost.sh

echoGC "wpdob2c is ready at $DNMP_DIR"
echoSB "Run vhost.sh to create or configure a B2C WordPress site."
