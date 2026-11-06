import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from './api';
import type { FleetEvent, Health } from './types';

/**
 * 通用数据拉取 hook：loading / error / reload。
 * enabled=false 时不发起请求（用于依赖选择的延迟加载）。
 */
export function useApi<T>(fn: () => Promise<T>, deps: readonly unknown[], enabled = true) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  const fnRef = useRef(fn);
  fnRef.current = fn;

  const reload = useCallback(async () => {
    try {
      const d = await fnRef.current();
      setData(d);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    setLoading(true);
    void reload();
    // eslint 缺失无妨（未配置 eslint），deps 由调用方声明
    // biome-ignore-all helper
  }, [...deps, enabled, reload]);

  return { data, loading, error, reload, setData };
}

/** 健康状态轮询（10s），后端不可达时 error 非空 */
export function useHealth() {
  const h = useApi<Health>(() => api.health(), []);
  useEffect(() => {
    const t = setInterval(() => void h.reload(), 10_000);
    return () => clearInterval(t);
  }, [h.reload]);
  return h;
}

/**
 * 全局事件流 SSE（浏览器原生 EventSource，断线自动重连）。
 * onEvent 变化不会重建连接。
 */
export function useEventSource(onEvent: (ev: FleetEvent) => void, enabled = true) {
  const cbRef = useRef(onEvent);
  cbRef.current = onEvent;
  useEffect(() => {
    if (!enabled) return;
    const es = new EventSource('/api/v1/events');
    es.onmessage = (e: MessageEvent<string>) => {
      try {
        cbRef.current(JSON.parse(e.data) as FleetEvent);
      } catch {
        // 非 JSON 心跳/噪声，忽略
      }
    };
    return () => es.close();
  }, [enabled]);
}

/** SSE 触发的节流刷新：最短间隔内只刷一次 */
export function useThrottledReload(reload: () => void | Promise<void>, minMs = 800) {
  const lastRef = useRef(0);
  return useCallback(() => {
    const now = Date.now();
    if (now - lastRef.current < minMs) return;
    lastRef.current = now;
    void reload();
  }, [reload, minMs]);
}
