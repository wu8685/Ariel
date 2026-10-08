#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

readonly managed_label="io.github.wu8685.ariel.managed=ecs"
readonly relay_container="ariel-relay"
readonly proxy_container="ariel-caddy"
readonly docker_network="ariel-ecs"

command_name="up"
state_dir="${ARIEL_ECS_STATE_DIR:-/opt/ariel}"
mode=""
origin=""
domain=""
rp_id=""
publish_port="8080"
http_port="80"
https_port="443"
relay_image="ariel-relay:ecs"
caddy_image="caddy:2-alpine"
image_source="build"
install_docker="0"
follow_logs="0"
log_service="relay"

mode_explicit="0"
origin_explicit="0"
domain_explicit="0"
rp_id_explicit="0"
port_explicit="0"
http_port_explicit="0"
https_port_explicit="0"
image_explicit="0"
build_explicit="0"
caddy_image_explicit="0"

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(cd -- "$script_dir/.." && pwd -P)"

usage() {
  cat <<'USAGE'
Ariel ECS 单机部署器

用法：
  ./scripts/deploy-ecs.sh up --mode pin --origin http://10.0.0.8:8080 [选项]
  ./scripts/deploy-ecs.sh up --mode passkey --domain ariel.example.com [选项]
  ./scripts/deploy-ecs.sh status [--state-dir /opt/ariel]
  ./scripts/deploy-ecs.sh show [--state-dir /opt/ariel]
  ./scripts/deploy-ecs.sh logs [--service relay|proxy] [--follow]
  ./scripts/deploy-ecs.sh disable-bootstrap [--state-dir /opt/ariel]
  ./scripts/deploy-ecs.sh down [--state-dir /opt/ariel]

命令：
  up                 构建或拉取镜像，生成凭据并启动服务
  status             显示脚本管理的容器状态
  show               显示访问地址、PIN（PIN 模式）和私有凭据文件位置
  logs               显示 Relay 或 Caddy 日志
  disable-bootstrap  首个 Passkey 登记后移除 setup token，并重建 Relay
  down               删除脚本管理的容器和空 network；保留全部状态与凭据

选项：
  --state-dir DIR       状态目录，默认 /opt/ariel；必须是绝对路径且不能是 /
  --mode pin            可信私网 PIN 模式；不要直接暴露到公网
  --mode passkey        公网 Passkey 模式；由 Caddy 自动提供 HTTPS/WSS
  --origin ORIGIN       PIN 模式的精确 http Origin，例如 http://10.0.0.8:8080
  --domain DOMAIN       Passkey 公共域名，不含 scheme、path 或 port
  --rp-id DOMAIN        WebAuthn RP ID；默认等于 --domain
  --port PORT           PIN 模式宿主机端口，默认 8080
  --http-port PORT      Caddy HTTP 宿主机端口，默认 80
  --https-port PORT     Caddy HTTPS/HTTP3 宿主机端口，默认 443
  --image IMAGE         Relay 镜像；默认 ariel-relay:ecs
  --build               从当前仓库构建 --image；否则显式镜像会从 Registry 拉取
  --caddy-image IMAGE   Caddy 镜像，默认 caddy:2-alpine
  --install-docker      Docker 缺失时用系统包管理器安装（需要 root）
  --service NAME        logs 的目标：relay 或 proxy
  --follow              持续跟随日志
  -h, --help            显示帮助

首次 up 会在状态目录生成 Agent token 和相应的 Web 凭据。重复 up 会复用它们，
不会静默切换认证模式或公共 Origin。生产镜像建议使用不可变 tag 或 digest。
USAGE
}

die() {
  printf 'Ariel ECS: %s\n' "$*" >&2
  exit 1
}

require_value() {
  local option_name="$1"
  local option_value="${2:-}"
  [[ -n "$option_value" ]] || die "$option_name 需要一个值。"
}

if [[ $# -gt 0 && "$1" != -* ]]; then
  command_name="$1"
  shift
fi

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    --state-dir)
      require_value "$1" "${2:-}"
      state_dir="$2"
      shift 2
      ;;
    --mode)
      require_value "$1" "${2:-}"
      mode="$2"
      mode_explicit="1"
      shift 2
      ;;
    --origin)
      require_value "$1" "${2:-}"
      origin="$2"
      origin_explicit="1"
      shift 2
      ;;
    --domain)
      require_value "$1" "${2:-}"
      domain="$2"
      domain_explicit="1"
      shift 2
      ;;
    --rp-id)
      require_value "$1" "${2:-}"
      rp_id="$2"
      rp_id_explicit="1"
      shift 2
      ;;
    --port)
      require_value "$1" "${2:-}"
      publish_port="$2"
      port_explicit="1"
      shift 2
      ;;
    --http-port)
      require_value "$1" "${2:-}"
      http_port="$2"
      http_port_explicit="1"
      shift 2
      ;;
    --https-port)
      require_value "$1" "${2:-}"
      https_port="$2"
      https_port_explicit="1"
      shift 2
      ;;
    --image)
      require_value "$1" "${2:-}"
      relay_image="$2"
      image_explicit="1"
      image_source="pull"
      shift 2
      ;;
    --build)
      build_explicit="1"
      image_source="build"
      shift
      ;;
    --caddy-image)
      require_value "$1" "${2:-}"
      caddy_image="$2"
      caddy_image_explicit="1"
      shift 2
      ;;
    --install-docker)
      install_docker="1"
      shift
      ;;
    --service)
      require_value "$1" "${2:-}"
      log_service="$2"
      shift 2
      ;;
    --follow)
      follow_logs="1"
      shift
      ;;
    *)
      die "未知参数：$1。运行 --help 查看用法。"
      ;;
  esac
done

case "$command_name" in
  up|status|show|logs|disable-bootstrap|down) ;;
  *) die "未知命令：$command_name。运行 --help 查看用法。" ;;
esac

validate_state_dir() {
  [[ "$state_dir" == /* ]] || die "状态目录必须是绝对路径：$state_dir"
  [[ "$state_dir" != "/" ]] || die "状态目录不能是根目录 /。"
  [[ "$state_dir" != *$'\n'* && "$state_dir" != *$'\r'* ]] || die "状态目录包含非法换行。"
  [[ ! -L "$state_dir" ]] || die "状态目录不能是符号链接：$state_dir"
}

validate_state_dir

config_file="$state_dir/config.env"
runtime_env_file="$state_dir/runtime.env"
agent_token_file="$state_dir/agent-token"
web_pin_file="$state_dir/web-pin"
session_key_file="$state_dir/session-key"
setup_token_file="$state_dir/passkey-setup-token"
credentials_file="$state_dir/data/auth.json"
caddyfile="$state_dir/Caddyfile"

stored_mode=""
stored_origin=""
stored_domain=""
stored_rp_id=""
stored_port=""
stored_http_port=""
stored_https_port=""
stored_relay_image=""
stored_caddy_image=""
stored_image_source=""

load_config() {
  [[ -f "$config_file" ]] || return 1
  local config_key config_value
  while IFS='=' read -r config_key config_value; do
    case "$config_key" in
      MODE) stored_mode="$config_value" ;;
      ORIGIN) stored_origin="$config_value" ;;
      DOMAIN) stored_domain="$config_value" ;;
      RP_ID) stored_rp_id="$config_value" ;;
      PORT) stored_port="$config_value" ;;
      HTTP_PORT) stored_http_port="$config_value" ;;
      HTTPS_PORT) stored_https_port="$config_value" ;;
      RELAY_IMAGE) stored_relay_image="$config_value" ;;
      CADDY_IMAGE) stored_caddy_image="$config_value" ;;
      IMAGE_SOURCE) stored_image_source="$config_value" ;;
      '') ;;
      *) die "状态配置包含未知字段 $config_key；请人工检查 $config_file。" ;;
    esac
  done < "$config_file"
  [[ -n "$stored_mode" && -n "$stored_origin" && -n "$stored_relay_image" ]] || \
    die "状态配置不完整：$config_file"
}

resolve_config() {
  if load_config; then
    if [[ "$mode_explicit" == "1" && "$mode" != "$stored_mode" ]]; then
      die "已有部署模式为 $stored_mode，拒绝静默切换为 $mode。请使用新的 --state-dir。"
    fi
    if [[ "$origin_explicit" == "1" && "$origin" != "$stored_origin" ]]; then
      die "公共 Origin 与已有部署不一致。WebAuthn 身份不可静默迁移，请使用新的 --state-dir。"
    fi
    if [[ "$domain_explicit" == "1" && "$domain" != "$stored_domain" ]]; then
      die "公共域名与已有部署不一致。请使用新的 --state-dir。"
    fi
    if [[ "$rp_id_explicit" == "1" && "$rp_id" != "$stored_rp_id" ]]; then
      die "RP ID 与已有部署不一致。请使用新的 --state-dir。"
    fi
    if [[ "$port_explicit" == "1" && "$publish_port" != "$stored_port" ]]; then
      die "PIN 发布端口与已有部署不一致。请使用新的 --state-dir。"
    fi
    if [[ "$http_port_explicit" == "1" && "$http_port" != "$stored_http_port" ]]; then
      die "HTTP 端口与已有部署不一致。请使用新的 --state-dir。"
    fi
    if [[ "$https_port_explicit" == "1" && "$https_port" != "$stored_https_port" ]]; then
      die "HTTPS 端口与已有部署不一致。请使用新的 --state-dir。"
    fi

    mode="$stored_mode"
    origin="$stored_origin"
    domain="$stored_domain"
    rp_id="$stored_rp_id"
    publish_port="$stored_port"
    http_port="$stored_http_port"
    https_port="$stored_https_port"
    if [[ "$image_explicit" != "1" ]]; then
      relay_image="$stored_relay_image"
    fi
    if [[ "$build_explicit" != "1" && "$image_explicit" != "1" ]]; then
      image_source="$stored_image_source"
    fi
    if [[ "$caddy_image_explicit" != "1" ]]; then
      caddy_image="$stored_caddy_image"
    fi
  else
    [[ "$command_name" == "up" ]] || die "尚未初始化：$config_file。请先运行 up。"
    [[ "$mode_explicit" == "1" ]] || die "首次 up 必须指定 --mode pin 或 --mode passkey。"
    if [[ "$mode" == "passkey" ]]; then
      [[ "$domain_explicit" == "1" ]] || die "Passkey 模式首次 up 必须指定 --domain。"
      origin="https://$domain"
      [[ -n "$rp_id" ]] || rp_id="$domain"
    else
      [[ "$origin_explicit" == "1" ]] || die "PIN 模式首次 up 必须指定 --origin。"
    fi
  fi
}

validate_port() {
  local port_name="$1"
  local port_value="$2"
  [[ "$port_value" =~ ^[0-9]+$ ]] || die "$port_name 必须是 1-65535 的整数。"
  (( port_value >= 1 && port_value <= 65535 )) || die "$port_name 必须是 1-65535 的整数。"
}

validate_domain() {
  local domain_value="$1"
  [[ ${#domain_value} -le 253 && "$domain_value" == *.* ]] || die "域名必须是完整 DNS host：$domain_value"
  [[ "$domain_value" != *".."* ]] || die "域名包含空 label：$domain_value"
  local domain_label
  local -a domain_labels
  IFS='.' read -r -a domain_labels <<< "$domain_value"
  for domain_label in "${domain_labels[@]}"; do
    [[ ${#domain_label} -le 63 && "$domain_label" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] || \
      die "域名 label 非法：$domain_label"
  done
}

validate_image() {
  local image_name="$1"
  [[ -n "$image_name" && "$image_name" != -* && "$image_name" != *[[:space:]]* ]] || die "镜像名非法：$image_name"
}

validate_config() {
  [[ "$mode" == "pin" || "$mode" == "passkey" ]] || die "--mode 只能是 pin 或 passkey。"
  validate_port "--port" "$publish_port"
  validate_port "--http-port" "$http_port"
  validate_port "--https-port" "$https_port"
  validate_image "$relay_image"
  validate_image "$caddy_image"
  [[ "$image_source" == "build" || "$image_source" == "pull" ]] || die "镜像来源状态非法。"

  if [[ "$mode" == "pin" ]]; then
    [[ "$origin" == http://* ]] || die "PIN Origin 必须使用 http://。"
    local origin_rest="${origin#http://}"
    [[ -n "$origin_rest" && "$origin_rest" != *'/'* && "$origin_rest" != *'?'* && "$origin_rest" != *'#'* && "$origin_rest" != *'@'* ]] || \
      die "PIN Origin 必须是无 path、query、fragment 或凭据的精确 Origin。"
    if [[ "$publish_port" == "80" ]]; then
      [[ "$origin_rest" != *:* || "$origin_rest" == *":80" ]] || die "PIN Origin 端口必须与 --port 一致。"
    else
      [[ "$origin_rest" == *":$publish_port" ]] || die "PIN Origin 端口必须与 --port $publish_port 一致。"
    fi
    [[ -z "$domain" && -z "$rp_id" ]] || die "PIN 模式不能配置 Passkey domain 或 RP ID。"
  else
    validate_domain "$domain"
    validate_domain "$rp_id"
    [[ "$domain" == "$rp_id" || "$domain" == *."$rp_id" ]] || die "RP ID 必须等于公共域名或其 DNS 父域。"
    [[ "$origin" == "https://$domain" ]] || die "Passkey 公共 Origin 必须精确等于 https://$domain。"
  fi
}

resolve_config
validate_config

atomic_write() {
  local target_file="$1"
  local target_content="$2"
  local temporary_file="${target_file}.tmp.$$"
  mkdir -p -- "$(dirname -- "$target_file")"
  printf '%s' "$target_content" > "$temporary_file"
  chmod 0600 "$temporary_file"
  mv -f -- "$temporary_file" "$target_file"
}

save_config() {
  mkdir -p -- "$state_dir"
  chmod 0700 "$state_dir"
  local config_body
  config_body="$(printf '%s\n' \
    "MODE=$mode" \
    "ORIGIN=$origin" \
    "DOMAIN=$domain" \
    "RP_ID=$rp_id" \
    "PORT=$publish_port" \
    "HTTP_PORT=$http_port" \
    "HTTPS_PORT=$https_port" \
    "RELAY_IMAGE=$relay_image" \
    "CADDY_IMAGE=$caddy_image" \
    "IMAGE_SOURCE=$image_source")"
  atomic_write "$config_file" "$config_body"$'\n'
}

random_hex() {
  local byte_count="$1"
  od -An -N "$byte_count" -tx1 /dev/urandom | tr -d ' \n'
}

random_pin() {
  local random_number
  random_number="$(od -An -N 4 -tu4 /dev/urandom | tr -d ' \n')"
  printf '%06d' "$((random_number % 1000000))"
}

read_secret() {
  local secret_file="$1"
  local secret_value
  IFS= read -r secret_value < "$secret_file" || true
  printf '%s' "$secret_value"
}

validate_single_line_secret() {
  local secret_name="$1"
  local secret_value="$2"
  [[ "$secret_value" != *$'\n'* && "$secret_value" != *$'\r'* ]] || die "$secret_name 不能包含换行。"
}

ensure_secret() {
  local secret_file="$1"
  local environment_name="$2"
  local generator_name="$3"
  if [[ -f "$secret_file" ]]; then
    chmod 0600 "$secret_file"
    return
  fi
  local secret_value="${!environment_name:-}"
  if [[ -z "$secret_value" ]]; then
    secret_value="$($generator_name)"
  fi
  validate_single_line_secret "$environment_name" "$secret_value"
  atomic_write "$secret_file" "$secret_value"$'\n'
}

generate_agent_token() { random_hex 32; }
generate_session_key() { random_hex 32; }
generate_setup_token() { random_hex 32; }
generate_web_pin() { random_pin; }

ensure_secrets() {
  ensure_secret "$agent_token_file" ARIEL_TOKEN generate_agent_token
  local agent_token
  agent_token="$(read_secret "$agent_token_file")"
  [[ ${#agent_token} -ge 32 ]] || die "ARIEL_TOKEN 至少需要 32 个字符。"

  if [[ "$mode" == "pin" ]]; then
    ensure_secret "$web_pin_file" ARIEL_WEB_PIN generate_web_pin
    local web_pin
    web_pin="$(read_secret "$web_pin_file")"
    [[ "$web_pin" =~ ^[0-9]{6}$ ]] || die "ARIEL_WEB_PIN 必须恰好是 6 位数字。"
    [[ "$web_pin" != "$agent_token" ]] || die "ARIEL_WEB_PIN 不能和 ARIEL_TOKEN 相同。"
  else
    ensure_secret "$session_key_file" ARIEL_SESSION_KEY generate_session_key
    local session_key
    session_key="$(read_secret "$session_key_file")"
    [[ "$session_key" =~ ^[0-9A-Fa-f]{64}$ ]] || die "ARIEL_SESSION_KEY 必须是 64 个十六进制字符。"
    if [[ ! -s "$credentials_file" && ! -f "$setup_token_file" ]]; then
      ensure_secret "$setup_token_file" ARIEL_PASSKEY_SETUP_TOKEN generate_setup_token
    fi
    if [[ -f "$setup_token_file" ]]; then
      local setup_token
      setup_token="$(read_secret "$setup_token_file")"
      [[ ${#setup_token} -ge 32 ]] || die "ARIEL_PASSKEY_SETUP_TOKEN 至少需要 32 个字符。"
    fi
  fi
}

write_runtime_env() {
  local agent_token
  agent_token="$(read_secret "$agent_token_file")"
  local env_body
  if [[ "$mode" == "pin" ]]; then
    local web_pin
    web_pin="$(read_secret "$web_pin_file")"
    env_body="$(printf '%s\n' \
      "ARIEL_TOKEN=$agent_token" \
      "ARIEL_WEB_AUTH=pin" \
      "ARIEL_WEB_PIN=$web_pin" \
      "ARIEL_ORIGINS=$origin")"
  else
    local session_key
    session_key="$(read_secret "$session_key_file")"
    env_body="$(printf '%s\n' \
      "ARIEL_TOKEN=$agent_token" \
      "ARIEL_WEB_AUTH=passkey" \
      "ARIEL_ORIGINS=$origin" \
      "ARIEL_PUBLIC_ORIGIN=$origin" \
      "ARIEL_WEBAUTHN_RP_ID=$rp_id" \
      "ARIEL_WEBAUTHN_CREDENTIALS_FILE=/data/auth.json" \
      "ARIEL_SESSION_KEY=$session_key")"
    if [[ -f "$setup_token_file" ]]; then
      local setup_token
      setup_token="$(read_secret "$setup_token_file")"
      env_body+=$'\n'"ARIEL_PASSKEY_SETUP_TOKEN=$setup_token"
    fi
  fi
  atomic_write "$runtime_env_file" "$env_body"$'\n'
}

write_caddyfile() {
  local caddy_body
  caddy_body="$(printf '%s\n' \
    "$domain {" \
    "  encode zstd gzip" \
    "  reverse_proxy $relay_container:8080" \
    "}")"
  atomic_write "$caddyfile" "$caddy_body"$'\n'
  chmod 0600 "$caddyfile"
}

install_docker_engine() {
  [[ "$(id -u)" == "0" ]] || die "--install-docker 需要 root；请使用 sudo 重新运行。"
  if command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y docker.io
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y docker
  elif command -v yum >/dev/null 2>&1; then
    yum install -y docker
  else
    die "未识别受支持的包管理器；请先安装 Docker Engine。"
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now docker
  fi
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    [[ "$install_docker" == "1" ]] || die "未找到 Docker。请先安装，或以 root 传 --install-docker。"
    install_docker_engine
  fi
  if ! docker info >/dev/null 2>&1 && [[ "$install_docker" == "1" && "$(id -u)" == "0" ]] && command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now docker
  fi
  docker info >/dev/null 2>&1 || die "Docker daemon 不可用，或当前用户没有访问权限。"
}

prepare_relay_image() {
  if [[ "$image_source" == "build" ]]; then
    printf '构建 Relay 镜像 %s ...\n' "$relay_image"
    docker build -t "$relay_image" "$repo_root"
  else
    printf '拉取 Relay 镜像 %s ...\n' "$relay_image"
    docker pull "$relay_image"
  fi
}

prepare_passkey_directories() {
  mkdir -p -- "$state_dir/data" "$state_dir/caddy-data" "$state_dir/caddy-config"
  chmod 0700 "$state_dir/data" "$state_dir/caddy-data" "$state_dir/caddy-config"
  docker run --rm \
    --entrypoint /bin/sh \
    --user 0:0 \
    -v "$state_dir/data:/data" \
    "$relay_image" \
    -c 'chown 10001:10001 /data && chmod 0700 /data'
}

container_exists() {
  docker container inspect "$1" >/dev/null 2>&1
}

assert_managed_container() {
  local container_name="$1"
  local owner_label
  owner_label="$(docker container inspect --format '{{ index .Config.Labels "io.github.wu8685.ariel.managed" }}' "$container_name")"
  [[ "$owner_label" == "ecs" ]] || die "容器 $container_name 不由本脚本管理，拒绝接管或删除。"
}

remove_managed_container() {
  local container_name="$1"
  if container_exists "$container_name"; then
    assert_managed_container "$container_name"
    docker rm -f "$container_name" >/dev/null
  fi
}

network_exists() {
  docker network inspect "$docker_network" >/dev/null 2>&1
}

ensure_network() {
  if network_exists; then
    local owner_label
    owner_label="$(docker network inspect --format '{{ index .Labels "io.github.wu8685.ariel.managed" }}' "$docker_network")"
    [[ "$owner_label" == "ecs" ]] || die "Docker network $docker_network 不由本脚本管理，拒绝接管。"
    return
  fi
  docker network create --label "$managed_label" "$docker_network" >/dev/null
}

remove_managed_network() {
  if network_exists; then
    local owner_label
    owner_label="$(docker network inspect --format '{{ index .Labels "io.github.wu8685.ariel.managed" }}' "$docker_network")"
    [[ "$owner_label" == "ecs" ]] || die "Docker network $docker_network 不由本脚本管理，拒绝删除。"
    docker network rm "$docker_network" >/dev/null
  fi
}

start_relay() {
  remove_managed_container "$relay_container"
  local -a relay_args=(
    run -d
    --name "$relay_container"
    --label "$managed_label"
    --restart unless-stopped
    --env-file "$runtime_env_file"
    --user 10001:10001
    --read-only
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m
    --cap-drop ALL
    --security-opt no-new-privileges:true
  )
  if [[ "$mode" == "pin" ]]; then
    relay_args+=(--publish "0.0.0.0:$publish_port:8080")
  else
    ensure_network
    relay_args+=(--network "$docker_network" --volume "$state_dir/data:/data")
  fi
  docker "${relay_args[@]}" "$relay_image" >/dev/null
}

wait_for_relay() {
  local attempt
  for attempt in $(seq 1 30); do
    if docker exec "$relay_container" sh -c 'wget -q -O - http://127.0.0.1:8080/healthz | grep -qx ok' >/dev/null 2>&1; then
      return
    fi
    if [[ "$(docker container inspect --format '{{.State.Running}}' "$relay_container" 2>/dev/null || true)" != "true" ]]; then
      break
    fi
    sleep 1
  done
  printf 'Relay 未通过健康检查。最近日志：\n' >&2
  docker logs --tail 80 "$relay_container" >&2 || true
  die "Relay 启动失败；状态和凭据已保留。"
}

start_proxy() {
  ensure_network
  remove_managed_container "$proxy_container"
  if docker image inspect "$caddy_image" >/dev/null 2>&1; then
    printf '复用本地 Caddy 镜像 %s。\n' "$caddy_image"
  else
    printf '拉取 Caddy 镜像 %s ...\n' "$caddy_image"
    docker pull "$caddy_image"
  fi
  docker run -d \
    --name "$proxy_container" \
    --label "$managed_label" \
    --restart unless-stopped \
    --network "$docker_network" \
    --publish "0.0.0.0:$http_port:80" \
    --publish "0.0.0.0:$https_port:443" \
    --publish "0.0.0.0:$https_port:443/udp" \
    --read-only \
    --tmpfs /tmp:rw,nosuid,nodev,noexec,size=16m \
    --cap-drop ALL \
    --cap-add NET_BIND_SERVICE \
    --security-opt no-new-privileges:true \
    --volume "$caddyfile:/etc/caddy/Caddyfile:ro" \
    --volume "$state_dir/caddy-data:/data" \
    --volume "$state_dir/caddy-config:/config" \
    "$caddy_image" >/dev/null
}

show_connection() {
  printf '\nAriel 已配置：\n'
  printf '  浏览器地址：%s\n' "$origin"
  if [[ "$mode" == "pin" ]]; then
    printf '  6 位连接码：%s\n' "$(read_secret "$web_pin_file")"
    printf '  Agent Relay：%s/ws\n' "${origin/http:\/\//ws://}"
  else
    printf '  Agent Relay：wss://%s/ws\n' "$domain"
    if [[ -f "$setup_token_file" ]]; then
      printf '  首次登记 token：sudo cat %s\n' "$setup_token_file"
      printf '  登记完成后：sudo %s disable-bootstrap --state-dir %s\n' "$script_dir/deploy-ecs.sh" "$state_dir"
    else
      printf '  Passkey bootstrap：已关闭\n'
    fi
    printf '  Passkey 凭据：%s\n' "$credentials_file"
  fi
  printf '  Agent token 文件：%s\n' "$agent_token_file"
  printf '  注意：不要复制 runtime.env；只把 agent-token 通过受保护渠道交给 Desktop Agent。\n'
}

run_up() {
  save_config
  ensure_secrets
  write_runtime_env
  ensure_docker
  prepare_relay_image
  if [[ "$mode" == "passkey" ]]; then
    prepare_passkey_directories
    write_caddyfile
  fi
  start_relay
  wait_for_relay
  if [[ "$mode" == "passkey" ]]; then
    start_proxy
    printf 'Relay 已健康；Caddy 正在申请或复用 TLS 证书。请另行核对 DNS、安全组和 Caddy 日志。\n'
  else
    remove_managed_container "$proxy_container"
  fi
  show_connection
}

run_status() {
  ensure_docker
  local target_name
  for target_name in "$relay_container" "$proxy_container"; do
    if container_exists "$target_name"; then
      assert_managed_container "$target_name"
      docker container inspect --format '{{.Name}}  status={{.State.Status}}  started={{.State.StartedAt}}' "$target_name"
    elif [[ "$target_name" == "$relay_container" || "$mode" == "passkey" ]]; then
      printf '/%s  status=not-created\n' "$target_name"
    fi
  done
}

run_logs() {
  ensure_docker
  local target_name
  case "$log_service" in
    relay) target_name="$relay_container" ;;
    proxy) target_name="$proxy_container" ;;
    *) die "--service 只能是 relay 或 proxy。" ;;
  esac
  container_exists "$target_name" || die "容器 $target_name 不存在。"
  assert_managed_container "$target_name"
  local -a log_args=(logs --tail 200)
  [[ "$follow_logs" == "1" ]] && log_args+=(--follow)
  docker "${log_args[@]}" "$target_name"
}

run_down() {
  ensure_docker
  remove_managed_container "$proxy_container"
  remove_managed_container "$relay_container"
  remove_managed_network
  printf 'Ariel 容器已停止；状态与凭据保留在 %s。\n' "$state_dir"
}

run_disable_bootstrap() {
  [[ "$mode" == "passkey" ]] || die "disable-bootstrap 只适用于 Passkey 模式。"
  [[ -s "$credentials_file" ]] || die "尚未发现 $credentials_file；请先完成首个 Passkey 登记。"
  if [[ ! -f "$setup_token_file" ]]; then
    printf 'Passkey bootstrap 已关闭。\n'
    return
  fi
  rm -f -- "$setup_token_file"
  write_runtime_env
  ensure_docker
  prepare_relay_image
  prepare_passkey_directories
  start_relay
  wait_for_relay
  printf 'Passkey bootstrap 已关闭；已有 Passkey 和浏览器 session key 均已保留。\n'
}

case "$command_name" in
  up) run_up ;;
  status) run_status ;;
  show) show_connection ;;
  logs) run_logs ;;
  disable-bootstrap) run_disable_bootstrap ;;
  down) run_down ;;
esac
