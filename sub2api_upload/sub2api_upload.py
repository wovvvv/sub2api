"""
Sub2API 批量上传 + 账号巡检脚本
用法:
    python sub2api_upload.py                          # 上传 config.json 中 token_json_dir
    python sub2api_upload.py /path/to/tokens          # 指定目录
    python sub2api_upload.py /path/to/tokens --no-delete  # 上传后不删除文件
    配置 config.json 的 sub2api_cron_interval > 0 即启用定时巡检（单位：秒）
依赖: pip install curl_cffi
"""

import os
import sys
import re
import json
import hashlib
import time
import argparse
import random
from datetime import datetime
from curl_cffi import requests as curl_requests


# ================= 配置加载 =================

def _as_bool(value):
    if isinstance(value, bool):
        return value
    if value is None:
        return False
    return str(value).strip().lower() in {"1", "true", "yes", "y", "on"}


def load_config():
    """从 config.json 加载 sub2api 相关配置"""
    base_dir = os.path.dirname(os.path.abspath(__file__))
    config_path = os.path.join(base_dir, "config.json")

    if not os.path.isfile(config_path):
        print(f"[错误] 找不到配置文件: {config_path}")
        sys.exit(1)

    with open(config_path, "r", encoding="utf-8") as f:
        cfg = json.load(f)

    return {
        "sub2api_url": os.environ.get("SUB2API_URL", cfg.get("sub2api_url", "")),
        "sub2api_token": os.environ.get("SUB2API_TOKEN", cfg.get("sub2api_token", "")),
        "token_json_dir": cfg.get("token_json_dir", "codex_tokens"),
        "sub2api_proxy_id": int(os.environ.get("SUB2API_PROXY_ID", cfg.get("sub2api_proxy_id", 1))),
        "sub2api_proxy": os.environ.get("SUB2API_PROXY", cfg.get("sub2api_proxy", "")),
        "sub2api_concurrency": int(os.environ.get("SUB2API_CONCURRENCY", cfg.get("sub2api_concurrency", 10))),
        "sub2api_priority": int(os.environ.get("SUB2API_PRIORITY", cfg.get("sub2api_priority", 1))),
        "sub2api_rate_multiplier": int(os.environ.get("SUB2API_RATE_MULTIPLIER", cfg.get("sub2api_rate_multiplier", 1))),
        "sub2api_group_ids": cfg.get("sub2api_group_ids", []),
        "sub2api_model_mapping": cfg.get("sub2api_model_mapping", {}),
        "sub2api_cron_interval": int(cfg.get("sub2api_cron_interval", 0)),
        "proxy": cfg.get("proxy", ""),
    }


# ================= 代理策略 =================

def _resolve_upload_proxy_candidates(sub2api_proxy, default_proxy):
    """解析代理候选列表，沿用 ncs_register.py 的逻辑"""
    raw = (sub2api_proxy or "").strip()
    if raw:
        lowered = raw.lower()
        if lowered in {"direct", "none", "off", "false", "0"}:
            return [None]
        if lowered == "default":
            return [default_proxy or None, None]
        return [raw]
    if default_proxy:
        return [default_proxy, None]
    return [None]


# ================= 通用请求 =================

def _sub2api_request(method, path, cfg, json_data=None):
    """通用 Sub2API 请求封装，返回 (status_code, response_json_or_None)"""
    sub2api_base = cfg["sub2api_url"].rstrip("/")
    sub2api_token = cfg["sub2api_token"]
    url = f"{sub2api_base}{path}"

    proxy_candidates = []
    for item in _resolve_upload_proxy_candidates(cfg["sub2api_proxy"], cfg["proxy"]):
        if item not in proxy_candidates:
            proxy_candidates.append(item)

    last_err = None
    for proxy in proxy_candidates:
        session = None
        try:
            session = curl_requests.Session()
            if proxy:
                session.proxies = {"http": proxy, "https": proxy}

            resp = session.request(
                method, url,
                json=json_data,
                headers={
                    "Authorization": f"Bearer {sub2api_token}",
                    "Content-Type": "application/json",
                    "Accept": "application/json, text/plain, */*",
                },
                verify=False,
                timeout=30,
            )
            return resp.status_code, resp.json() if resp.text else None

        except Exception as e:
            last_err = e
            if proxy and proxy_candidates.index(proxy) < len(proxy_candidates) - 1:
                continue
            return -1, None
        finally:
            if session:
                try:
                    session.close()
                except Exception:
                    pass

    return -1, None


def _build_test_prompt():
    """随机生成测试 prompt，模拟正常用户提问"""
    prompts = [
        "1+1", "hi", "ok?", "yes or no?", "10*5",
        "2^10", "good morning", "thanks", "next", "go on",
        "alright", "got it", "sure", "okay", "no thanks",
        "hello", "bye", "done", "skip", "pass",
        "6+7", "3*3", "100/4", "2+2", "9*9",
        "understood", "roger", "fine", "true", "false",
        "haha", "right", "go", "ok", "no",
        "done", "next", "pass", "skip", "stop",
        "morning", "evening", "noon", "night", "hello",
        "bye", "thanks", "cool", "nice", "great",
        "whatever", "any", "none", "yes", "what",
        "how many", "who", "where", "how", "why",
        "1", "2", "3", "5", "8",
        "today", "tomorrow", "yesterday", "now", "later",
        "yes", "no", "maybe", "probably", "always",
        "red", "blue", "green", "white", "black",
        "cat", "dog", "fish", "bird", "flower",
        "water", "fire", "wind", "mountain", "sea",
        "big", "small", "more", "less", "fast",
        "slow", "high", "low", "far", "near",
        "7-3", "4*6", "50+50", "sqrt(144)", "15%4",
        "sup", "yo", "hey", "ah", "oh",
        "wow", "ooh", "uh", "mm", "hm",
        "please", "sorry", "excuse me", "pardon", "cheers",
        "sweet", "awesome", "perfect", "amazing", "brilliant",
        "wait", "hold on", "pause", "resume", "again",
        "start", "begin", "end", "finish", "close",
        "open", "show", "hide", "tell", "ask",
        "up", "down", "left", "right", "here",
        "apple", "banana", "orange", "grape", "lemon",
        "sun", "moon", "star", "rain", "snow",
        "spring", "summer", "autumn", "winter", "fall",
        "monday", "friday", "weekend", "holiday", "break",
        "book", "pen", "paper", "desk", "chair",
        "run", "walk", "sit", "stand", "jump",
        "eat", "drink", "sleep", "wake", "rest",
        "happy", "sad", "angry", "calm", "tired",
        "hot", "cold", "warm", "cool", "dry",
        "new", "old", "young", "long", "short",
        "easy", "hard", "simple", "quick", "free",
        "good", "bad", "best", "worst", "better",
        "full", "empty", "half", "zero", "one",
        "first", "last", "only", "both", "all",
    ]
    return random.choice(prompts)




def _parse_cron_field(field, min_value, max_value):
    """解析单个 cron 字段，返回允许值集合"""
    values = set()
    for part in field.split(","):
        part = part.strip()
        if not part:
            continue

        step = 1
        if "/" in part:
            base, step_text = part.split("/", 1)
            step = int(step_text)
        else:
            base = part

        if base == "*":
            start = min_value
            end = max_value
        elif "-" in base:
            start_text, end_text = base.split("-", 1)
            start = int(start_text)
            end = int(end_text)
        else:
            start = int(base)
            end = int(base)

        if start < min_value or end > max_value or start > end or step <= 0:
            raise ValueError(f"非法 cron 字段: {field}")

        values.update(range(start, end + 1, step))

    if not values:
        raise ValueError(f"空 cron 字段: {field}")
    return values


def _cron_matches(dt, cron_expr):
    """判断当前时间是否命中标准 5 段 cron 表达式：分 时 日 月 周"""
    parts = cron_expr.strip().split()
    if len(parts) != 5:
        raise ValueError("cron 表达式必须为 5 段：分 时 日 月 周")

    minute_set = _parse_cron_field(parts[0], 0, 59)
    hour_set = _parse_cron_field(parts[1], 0, 23)
    day_set = _parse_cron_field(parts[2], 1, 31)
    month_set = _parse_cron_field(parts[3], 1, 12)
    week_set = _parse_cron_field(parts[4].replace("7", "0"), 0, 6)
    week_day = (dt.weekday() + 1) % 7

    return (
        dt.minute in minute_set
        and dt.hour in hour_set
        and dt.day in day_set
        and dt.month in month_set
        and week_day in week_set
    )


def _seconds_until_next_minute(now):
    """计算距离下一分钟整点的等待秒数"""
    return 60 - now.second if now.second > 0 else 60


# ================= 查询账号列表 =================

def list_accounts(cfg, page=1, page_size=100):
    """查询 Sub2API 账号列表，返回 (total, accounts_list) 或 (-1, [])"""
    code, data = _sub2api_request(
        "GET",
        f"/api/v1/admin/accounts?page={page}&page_size={page_size}&timezone=Asia%2FShanghai",
        cfg,
    )
    if code == 200 and data:
        inner = data.get("data", data) if isinstance(data, dict) else data
        total = inner.get("total", -1)
        accounts = inner.get("items", [])
        return int(total), accounts
    return -1, []


def get_account_count(cfg):
    """查询 Sub2API 当前账号总数"""
    total, _ = list_accounts(cfg, page=1, page_size=1)
    if total >= 0:
        print(f"  [Sub2API] 📊 当前账号数量: {total}")
    else:
        print(f"  [Sub2API] ⚠️ 查询账号数量失败")
    return total


# ================= 删除账号 =================

def delete_account(account_id, cfg):
    """删除单个账号，返回 True=成功"""
    code, _ = _sub2api_request(
        "DELETE",
        f"/api/v1/admin/accounts/{account_id}",
        cfg,
    )
    return code in [200, 204]


# ================= 测试账号（流式响应） =================

def test_account(account_id, cfg):
    """测试单个账号是否有效，读取 SSE 流判断，返回 True=有效，False=无效"""
    sub2api_base = cfg["sub2api_url"].rstrip("/")
    sub2api_token = cfg["sub2api_token"]
    url = f"{sub2api_base}/api/v1/admin/accounts/{account_id}/test"
    test_prompt = _build_test_prompt()

    proxy_candidates = []
    for item in _resolve_upload_proxy_candidates(cfg["sub2api_proxy"], cfg["proxy"]):
        if item not in proxy_candidates:
            proxy_candidates.append(item)

    for proxy in proxy_candidates:
        session = None
        try:
            session = curl_requests.Session()
            if proxy:
                session.proxies = {"http": proxy, "https": proxy}

            resp = session.post(
                url,
                json={"model_id": "gpt-5.4", "prompt": test_prompt},
                headers={
                    "Authorization": f"Bearer {sub2api_token}",
                    "Content-Type": "application/json",
                    "Accept": "application/json, text/plain, */*",
                },
                verify=False,
                timeout=30,
            )

            # 流式响应是多行 JSON，直接读 text 按行解析
            body = resp.text

            for raw_line in body.splitlines():
                raw_line = raw_line.strip()
                if not raw_line:
                    continue
                # 去掉 SSE 的 "data: " 前缀
                if raw_line.startswith("data: "):
                    raw_line = raw_line[6:]
                elif raw_line.startswith("data:"):
                    raw_line = raw_line[5:]
                try:
                    event = json.loads(raw_line)
                except json.JSONDecodeError:
                    continue
                event_type = event.get("type", "")
                if event_type == "error":
                    error_msg = event.get("error", "")
                    if re.search(r'"status"\s*:\s*(401|429)', error_msg) or \
                       re.search(r'API returned (401|429)', error_msg):
                        return False
                    return True

            # 流中没有 error 事件，视为有效
            return True

        except Exception as e:
            continue
        finally:
            if session:
                try:
                    session.close()
                except Exception:
                    pass

    # 所有代理都失败，不贸然删除
    return True

def upload_token_to_sub2api(filepath, cfg):
    """上传单个 token 文件到 Sub2API"""
    sub2api_base = cfg["sub2api_url"].rstrip("/")
    sub2api_token = cfg["sub2api_token"]

    if not sub2api_base or not sub2api_token:
        print("  [Sub2API] ⚠️ 未配置 sub2api_url 或 sub2api_token，跳过")
        return False

    filename = os.path.basename(filepath)

    # 读取 token 数据
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            token_data = json.load(f)
    except Exception as e:
        print(f"  [Sub2API] ❌ {filename} 读取失败: {e}")
        return False

    # 构造 Sub2API 请求格式
    credentials = {
        "access_token": token_data.get("access_token", ""),
        "expires_at": token_data.get("expires_at"),
        "refresh_token": token_data.get("refresh_token", ""),
        "id_token": token_data.get("id_token", ""),
        "email": token_data.get("email", ""),
        "chatgpt_account_id": token_data.get("account_id", ""),
    }

    # 添加可选字段
    if token_data.get("chatgpt_user_id"):
        credentials["chatgpt_user_id"] = token_data["chatgpt_user_id"]
    if token_data.get("organization_id"):
        credentials["organization_id"] = token_data["organization_id"]
    if token_data.get("plan_type"):
        credentials["plan_type"] = token_data["plan_type"]
    if token_data.get("client_id"):
        credentials["client_id"] = token_data["client_id"]
    if cfg["sub2api_model_mapping"]:
        credentials["model_mapping"] = cfg["sub2api_model_mapping"]

    # 构造请求体
    email_short = hashlib.md5(token_data.get("email", "").encode()).hexdigest()[:8]

    request_body = {
        "name": email_short,
        "notes": f"Auto-registered via cpa_20260323 on {token_data.get('last_refresh', '')}",
        "platform": "openai",
        "type": "oauth",
        "credentials": credentials,
        "extra": {
            "email": token_data.get("email", ""),
            "privacy_mode": "training_off",
            "openai_oauth_responses_websockets_v2_mode": "off",
            "openai_oauth_responses_websockets_v2_enabled": False,
        },
        "proxy_id": cfg["sub2api_proxy_id"],
        "concurrency": cfg["sub2api_concurrency"],
        "priority": cfg["sub2api_priority"],
        "rate_multiplier": cfg["sub2api_rate_multiplier"],
        "group_ids": cfg["sub2api_group_ids"] if cfg["sub2api_group_ids"] else [],
        "expires_at": None,
        "auto_pause_on_expired": True,
    }

    # 调用通用请求（curl 会自动打印）
    print(f"  [Sub2API] 📤 {filename}")
    code, resp_data = _sub2api_request("POST", "/api/v1/admin/accounts", cfg, json_data=request_body)

    if code in [200, 201]:
        print(f"  [Sub2API] ✅ {filename} 已上传到 Sub2API")
        return True
    elif code > 0:
        print(f"  [Sub2API] ❌ {filename} 上传失败: {code}")
        return False
    else:
        print(f"  [Sub2API] ❌ {filename} 上传异常: 网络错误")
        return False
    return False


# ================= 批量上传 =================

def upload_all_tokens(token_dir, cfg, no_delete=False):
    """扫描目录并批量上传所有 token JSON 文件（账号少于5个时上传，达到10个后停止）"""
    MIN_ACCOUNTS = 5   # 少于此数才开始上传
    MAX_ACCOUNTS = 10  # 达到此数停止上传

    sub2api_base = cfg["sub2api_url"].rstrip("/")
    sub2api_token = cfg["sub2api_token"]

    if not sub2api_base or not sub2api_token:
        print("[Sub2API] ⚠️ 未配置 sub2api_url 或 sub2api_token，跳过 Sub2API 上传")
        return

    # 解析目录路径（命令行参数 > config.json > codex_tokens）
    base_dir = os.path.dirname(os.path.abspath(__file__))
    if not token_dir:
        token_dir = cfg.get("token_json_dir", "codex_tokens")
    elif not os.path.isabs(token_dir):
        token_dir = os.path.join(base_dir, token_dir)

    if not os.path.isdir(token_dir):
        print(f"[Sub2API] ⚠️ 目录不存在: {token_dir}")
        return

    json_files = [f for f in os.listdir(token_dir) if f.endswith(".json")]
    if not json_files:
        print("[Sub2API] 📭 没有待上传的 token 文件")
        return

    # 查询当前账号数量
    print(f"\n[Sub2API] 正在查询当前账号数量...")
    current_count = get_account_count(cfg)

    if current_count >= MAX_ACCOUNTS:
        print(f"[Sub2API] ⏸️ 当前已有 {current_count} 个账号（>= {MAX_ACCOUNTS}），无需上传")
        return
    if current_count < 0:
        print("[Sub2API] ⚠️ 无法获取账号数量，跳过本次上传")
        return
    if current_count >= MIN_ACCOUNTS:
        print(f"[Sub2API] ⏸️ 当前已有 {current_count} 个账号（>= {MIN_ACCOUNTS}），无需上传")
        return

    can_upload = MAX_ACCOUNTS - current_count

    print(f"\n{'=' * 60}")
    print(f"  [Sub2API] 当前账号: {current_count} 个, 还可上传: {can_upload} 个")
    print(f"  [Sub2API] 待上传文件: {len(json_files)} 个")
    print(f"  [Sub2API] 目标目录: {token_dir}")
    print(f"  [Sub2API] 上传后删除: {'否' if no_delete else '是'}")
    print(f"{'=' * 60}")

    uploaded = 0
    failed = 0

    for filename in json_files:
        if uploaded >= can_upload:
            print(f"\n  [Sub2API] ⏸️ 已上传 {uploaded} 个，达到上限 {MAX_ACCOUNTS}，停止上传")
            break
        filepath = os.path.join(token_dir, filename)
        if upload_token_to_sub2api(filepath, cfg):
            if not no_delete:
                try:
                    os.remove(filepath)
                except Exception:
                    pass
            uploaded += 1
        else:
            failed += 1

    print(f"\n  [Sub2API] 上传完成: 成功 {uploaded} 个, 失败 {failed} 个")
    print(f"{'=' * 60}\n")


# ================= 账号巡检 =================

def check_and_clean_accounts(cfg):
    """巡检所有账号，删除 401/429 的无效账号"""
    print(f"  [巡检] 开始检测账号有效性...")
    total, accounts = list_accounts(cfg, page_size=100)

    if total < 0:
        print(f"  [巡检] ⚠️ 获取账号列表失败，跳过巡检")
        return

    # 分页处理所有账号
    all_accounts = list(accounts)
    pages = max(1, (total + 99) // 100)
    for page in range(2, pages + 1):
        _, more = list_accounts(cfg, page=page, page_size=100)
        all_accounts.extend(more)

    if not all_accounts:
        print(f"  [巡检] 📭 没有账号需要检测")
        return

    print(f"  [巡检] 共 {len(all_accounts)} 个账号待检测")

    deleted = 0
    alive = 0

    for account in all_accounts:
        account_id = account.get("id")
        if not account_id:
            continue

        name = account.get("name", account.get("email", str(account_id)))
        valid = test_account(account_id, cfg)

        if valid:
            alive += 1
            print(f"  [巡检] ✅ 账号 {name}(ID:{account_id}) 有效")
        else:
            print(f"  [巡检] ❌ 账号 {name}(ID:{account_id}) 无效(401/429)，正在删除...")
            if delete_account(account_id, cfg):
                deleted += 1
                print(f"  [巡检] 🗑️ 账号 {name}(ID:{account_id}) 已删除")
            else:
                print(f"  [巡检] ⚠️ 账号 {name}(ID:{account_id}) 删除失败")

    print(f"  [巡检] 检测完成: 有效 {alive} 个, 删除 {deleted} 个")
    return deleted


# ================= Cron 主循环 =================

def run_cron(interval, cfg):
    """定期巡检 + 按需上传"""
    print(f"\n{'=' * 60}")
    print(f"  [Cron] 已启动，每 {interval} 秒执行一次巡检 + 按需上传")
    print(f"  [Cron] 按 Ctrl+C 停止")
    print(f"{'=' * 60}\n")

    while True:
        try:
            now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
            print(f"\n{'─' * 40}")
            print(f"  [Cron] {now} 开始执行...")
            print(f"{'─' * 40}")

            # 1. 巡检并清理无效账号
            check_and_clean_accounts(cfg)

            # 2. 按需上传
            token_dir = cfg.get("token_json_dir", "codex_tokens")
            upload_all_tokens(token_dir, cfg)

            print(f"  [Cron] 本轮执行完毕，{interval} 秒后再次执行...")
            time.sleep(interval)

        except KeyboardInterrupt:
            print(f"\n  [Cron] 已停止")
            break
        except Exception as e:
            print(f"  [Cron] ❌ 执行异常: {e}")
            time.sleep(interval)


# ================= 入口 =================

def main():
    parser = argparse.ArgumentParser(description="Sub2API 批量上传 + 账号巡检工具")
    parser.add_argument("token_dir", nargs="?", default="", help="token JSON 文件目录（默认读取 config.json 的 token_json_dir）")
    parser.add_argument("--no-delete", action="store_true", help="上传成功后不删除本地 JSON 文件")
    args = parser.parse_args()

    cfg = load_config()

    interval = cfg.get("sub2api_cron_interval", 30)
    if interval > 0:
        run_cron(interval, cfg)
    else:
        upload_all_tokens(args.token_dir, cfg, no_delete=args.no_delete)


if __name__ == "__main__":
    main()
