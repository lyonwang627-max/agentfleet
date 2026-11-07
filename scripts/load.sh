#!/bin/bash
# 压测脚本（Q5）：并发创建任务 → 观察调度吞吐与限流行为。
# 用法：./scripts/load.sh [总任务数] [并发] [目标地址]
# 依赖：fleetd 运行中（--mem 模式即可）；用 curl 无外部压测工具。
set -e
TOTAL=${1:-50}
CONC=${2:-10}
BASE=${3:-http://localhost:8090}
API="$BASE/api/v1"

echo "── 预检：健康 ──"
HEALTH=$(curl -s --noproxy '*' "$API/health")
echo "$HEALTH" | grep -q '"status":"ok"' || { echo "后端不可用"; exit 1; }
echo "ok"

echo "── 建压测 agent（预算不限）──"
AID=$(curl -s --noproxy '*' -X POST "$API/agents" -H 'Content-Type: application/json' \
  -d '{"name":"load-tester","role":"压测","runner":"mock","model":"mock","max_concurrency":4}' \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
echo "agent#$AID"

echo "── 并发灌 $TOTAL 个任务（并发 $CONC）──"
START=$(python3 -c "import time; print(time.time())")
seq 1 "$TOTAL" | xargs -P "$CONC" -I{} curl -s --noproxy '*' -X POST "$API/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"title":"压测任务{}","prompt":"普通任务，直接完成。","agent_id":'"$AID"',"priority":5}' -o /dev/null
echo "灌入完成"

echo "── 等待调度消化 ──"
DONE=0
while [ "$DONE" -lt "$TOTAL" ] && [ $SECONDS -lt 120 ]; do
  DONE=$(curl -s --noproxy '*' "$API/tasks?status=done&page_size=1" | python3 -c "
import json,sys
d = json.load(sys.stdin)
import re
print(sum(1 for t in d.get('items',[]) ))" 2>/dev/null || echo 0)
  # 直接用全量统计更准确
  DONE=$(curl -s --noproxy '*' "$API/tasks?page_size=100" | python3 -c "
import json,sys
d = json.load(sys.stdin)
print(sum(1 for t in d.get('items',[]) if t['status']=='done'))")
  sleep 2
done

END=$(python3 -c "import time; print(time.time())")
ELAPSED=$(python3 -c "print(f'{$END-$START:.1f}')")
echo ""
echo "── 结果 ──"
curl -s --noproxy '*' "$API/tasks?page_size=100" | python3 -c "
import json,sys
d = json.load(sys.stdin)
from collections import Counter
c = Counter(t['status'] for t in d.get('items',[]))
print('状态分布:', dict(c))"
echo "总耗时: ${ELAPSED}s（$TOTAL 任务）"
echo "吞吐: $(python3 -c "print(f'{$TOTAL/$ELAPSED:.1f}')") 任务/秒（含灌入+调度+mock 执行）"
echo ""
echo "提示：进一步压测可启用限流（FLEET_RATE_LIMIT=60）观察 429 行为。"
