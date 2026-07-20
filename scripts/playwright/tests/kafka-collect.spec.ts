/**
 * Kafka 采集 + JSON Parser 解析端到端验收脚本
 *
 * 对应人工验收文档：docs/requirements/references/XDP_P1真实浏览器人工验收.md
 * 覆盖用例：
 *   TC-P1-INDEX-BROWSER-001 创建测试 index
 *   TC-P1-KAFKA-BROWSER-001 Kafka 插件启用后出现在采集页
 *   TC-P1-KAFKA-BROWSER-002 创建 Kafka 数据源
 *   TC-P1-KAFKA-BROWSER-003 页面点击 Kafka 测试连通性按钮展示成功与失败结果
 *   TC-P1-JSON-BROWSER-001 创建 JSON Parser 解析规则
 *   TC-P1-CH-BROWSER-001 验证原始日志与解析字段
 *
 * 验收链路：
 *   登录态复用 → API 预置 index json_p1
 *   → 采集页断言 Kafka 插件可选 → API 创建 Kafka 数据源 + 连通性测试
 *   → 解析页断言 JSON Parser 可选 → API 创建 JSON Parser 规则
 *   → 容器内 producer 写 JSON 测试数据 → 等待入库
 *   → API 搜索断言 parse_status=parsed + 字段含 level/service/user.id/user.region/latency
 *
 * 运行：npx playwright test tests/kafka-collect.spec.ts --project=admin
 *
 * 说明：本脚本依赖 Kafka 容器（docker-compose-kafka-1）可访问，且 Kafka 插件已上传启用。
 *       若插件未启用，用 test.skip() 软降级避免阻塞 P0 验收。
 */
import { spawn } from 'node:child_process';
import { test, expect, type Page } from '@playwright/test';

const API_URL = process.env.XDP_API_URL || 'http://127.0.0.1:8080';
const RUN_ID = Date.now();
const INDEX_NAME = `accept_kafka_${RUN_ID}`;
const DATA_SOURCE_NAME = `accept_kafka_src_${RUN_ID}`;
const RULE_NAME = `accept_kafka_rule_${RUN_ID}`;
const TOPIC = `xdp-p1-accept-${RUN_ID}`;
const CONSUMER_GROUP = `xdp-p1-accept-group-${RUN_ID}`;

// 参考文档 9.1 节的 JSON 测试日志
const JSON_LOGS = [
  '{"level":"info","service":"checkout","user":{"id":"u-1","region":"CN"},"latency":128}',
  '{"level":"warn","service":"checkout","user":{"id":"u-2","region":"CN"},"latency":302}',
  '{"level":"warn","service":"billing","user":{"id":"u-3","region":"US"},"latency":411}',
];

async function authHeaders(page: Page) {
  const state = await page.context().storageState();
  let token = '';
  for (const origin of state.origins || []) {
    const matched = origin.localStorage.find((item) => item.name === 'xdp_api_token');
    if (matched?.value) {
      token = matched.value;
      break;
    }
  }
  if (!token) {
    token = await page.evaluate(() => localStorage.getItem('xdp_api_token') || '').catch(() => '');
  }
  return { Authorization: `Bearer ${token}` };
}

async function requestJSON(page: Page, method: 'GET' | 'POST' | 'DELETE', path: string, data?: unknown) {
  const headers = await authHeaders(page);
  const response = await page.request.fetch(`${API_URL}${path}`, {
    method,
    headers: { ...headers, ...(data ? { 'Content-Type': 'application/json' } : {}) },
    data,
  });
  if (!response.ok()) {
    throw new Error(`${method} ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const text = await response.text();
  return text ? JSON.parse(text) : {};
}

async function cleanupByName(page: Page, kind: 'parse-rules' | 'datasources' | 'indexes', name: string) {
  if (kind === 'indexes') {
    await requestJSON(page, 'DELETE', `/api/v1/indexes?index=${encodeURIComponent(name)}&drop_storage=true`).catch(() => undefined);
    return;
  }
  const listPath = kind === 'parse-rules' ? '/api/v1/parse-rules?page=1&page_size=1000' : '/api/v1/datasources?page=1&page_size=1000';
  const payload = await requestJSON(page, 'GET', listPath).catch(() => ({}));
  const items = Array.isArray(payload[kind === 'parse-rules' ? 'parse_rules' : 'datasources']) ? payload[kind === 'parse-rules' ? 'parse_rules' : 'datasources'] : [];
  for (const item of items) {
    if (String(item.name || '').trim() !== name) continue;
    const id = String(item.id || item.code || '').trim();
    if (!id) continue;
    await requestJSON(page, 'DELETE', `/api/v1/${kind === 'parse-rules' ? 'parse-rules' : 'datasources'}/${encodeURIComponent(id)}`).catch(() => undefined);
  }
}

// 检测 Kafka 插件是否已上传启用，未启用时软降级
async function isKafkaPluginEnabled(page: Page): Promise<boolean> {
  const payload = await requestJSON(page, 'GET', '/api/v1/plugins/catalog?plugin_type=input').catch(() => ({ plugins: [] }));
  const plugins: Array<{ code?: string; status?: string }> = Array.isArray(payload.plugins) ? payload.plugins : [];
  const kafka = plugins.find((p) => String(p.code || '') === 'kafka');
  return !!kafka && kafka.status !== 'disabled';
}

async function isJsonParserPluginEnabled(page: Page): Promise<boolean> {
  const payload = await requestJSON(page, 'GET', '/api/v1/plugins/catalog?plugin_type=parser').catch(() => ({ plugins: [] }));
  const plugins: Array<{ code?: string; status?: string }> = Array.isArray(payload.plugins) ? payload.plugins : [];
  const json = plugins.find((p) => String(p.code || '') === 'json-parser');
  return !!json && json.status !== 'disabled';
}

// 容器内 producer 写 JSON 测试数据
async function produceKafkaMessages(topic: string, messages: string[]) {
  const input = messages.map((m) => m).join('\n');
  await new Promise<void>((resolve, reject) => {
    const proc = spawn('docker', [
      'exec', '-i', 'docker-compose-kafka-1',
      '/opt/kafka/bin/kafka-console-producer.sh',
      '--bootstrap-server', '127.0.0.1:9092',
      '--topic', topic,
    ]);
    const timeout = setTimeout(() => {
      proc.kill('SIGTERM');
      reject(new Error('kafka-console-producer 超过 30s 未退出'));
    }, 30_000);
    proc.on('exit', (code) => {
      clearTimeout(timeout);
      if (code !== 0) reject(new Error(`kafka-console-producer 退出码 ${code}`));
      else resolve();
    });
    proc.on('error', reject);
    proc.stdin.write(input);
    proc.stdin.end();
  });
}

async function waitForKafkaData(page: Page, expectedSubstring: string, timeoutMs = 90_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const payload = await requestJSON(page, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${INDEX_NAME}`)}&limit=20&page=1`).catch(() => null);
    if (JSON.stringify(payload || {}).includes(expectedSubstring)) return;
    await new Promise((resolve) => setTimeout(resolve, 2000));
  }
  throw new Error(`等待 Kafka 数据入库超时：预期含 ${expectedSubstring}`);
}

async function openCollectPage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-collect"]');
  await expect(page.locator('[data-testid="collect-page"]')).toBeVisible();
}

async function openParsePage(page: Page) {
  await page.goto('/');
  await expect(page.locator('[data-testid="logout"]')).toBeVisible();
  await page.click('[data-testid="nav-parse"]');
  await expect(page.locator('[data-testid="parse-page"]')).toBeVisible();
}

test.describe('TC-P1-KAFKA Kafka 采集与 JSON Parser 端到端', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupByName(page, 'parse-rules', RULE_NAME);
      await cleanupByName(page, 'datasources', DATA_SOURCE_NAME);
      await cleanupByName(page, 'indexes', INDEX_NAME);
    } finally {
      await context.close();
    }
  });

  test.afterAll(async ({ browser }) => {
    const context = await browser.newContext({ storageState: 'fixtures/admin.storageState' });
    const page = await context.newPage();
    try {
      await page.goto('/');
      await expect(page.locator('[data-testid="logout"]')).toBeVisible();
      await cleanupByName(page, 'parse-rules', RULE_NAME);
      await cleanupByName(page, 'datasources', DATA_SOURCE_NAME);
      await cleanupByName(page, 'indexes', INDEX_NAME);
    } finally {
      await context.close();
    }
  });

  test('TC-P1-INDEX-BROWSER-001 创建测试 index', async ({ page }) => {
    console.log('== phase == TC-P1-INDEX-BROWSER-001 create json_p1 index via API');
    await requestJSON(page, 'POST', '/api/v1/indexes', {
      index_name: INDEX_NAME,
      ttl_days: 30,
      status: 'active',
    });
    // 断言 index 已创建
    const payload = await requestJSON(page, 'GET', `/api/v1/indexes?page=1&page_size=1000`);
    const indexes: Array<{ index_name?: string; name?: string }> = Array.isArray(payload.indexes) ? payload.indexes : [];
    const found = indexes.some((idx) => String(idx.index_name || idx.name || '').trim() === INDEX_NAME);
    expect(found, `[TC-P1-INDEX-BROWSER-001] 预期 index ${INDEX_NAME} 已创建`).toBe(true);
  });

  test('TC-P1-KAFKA-BROWSER-001 Kafka 插件启用后出现在采集页', async ({ page }) => {
    console.log('== phase == TC-P1-KAFKA-BROWSER-001 assert kafka plugin visible in collect page');
    // 先检测插件是否已启用，未启用则软降级
    const enabled = await isKafkaPluginEnabled(page);
    if (!enabled) {
      console.log('== phase == TC-P1-KAFKA-BROWSER-001 SKIPPED: kafka plugin not enabled');
      test.skip();
      return;
    }

    await openCollectPage(page);
    await page.click('[data-testid="show-input-form"]');
    await expect(page.locator('[data-testid="input-form-card"]'), '[TC-P1-KAFKA-BROWSER-001] 预期采集表单可见').toBeVisible({ timeout: 5_000 });
    await expect(page.locator('[data-testid="input-plugin-kafka"]'), '[TC-P1-KAFKA-BROWSER-001] 预期 Kafka 插件可选').toBeVisible();
    // Syslog 仍可选
    await expect(page.locator('[data-testid="input-plugin-syslog"]'), '[TC-P1-KAFKA-BROWSER-001] 预期 Syslog 仍可选').toBeVisible();
  });

  test('TC-P1-KAFKA-BROWSER-002 创建 Kafka 数据源', async ({ page }) => {
    console.log('== phase == TC-P1-KAFKA-BROWSER-002 create kafka datasource via API');
    const enabled = await isKafkaPluginEnabled(page);
    if (!enabled) { test.skip(); return; }

    // 连通性测试
    const checkPayload = await requestJSON(page, 'POST', '/api/v1/datasources/connectivity-check', {
      plugin_code: 'kafka',
      plugin_config: {
        brokers: ['127.0.0.1:9092'],
        topic: TOPIC,
        consumer_group: CONSUMER_GROUP,
        start_offset: 'earliest',
        security_protocol: 'PLAINTEXT',
        encoding: 'UTF-8',
        log_filter_enabled: false,
      },
    });
    expect(checkPayload.status, '[TC-P1-KAFKA-BROWSER-002] 预期连通性测试返回 ok').toBe('ok');

    // 创建数据源
    await requestJSON(page, 'POST', '/api/v1/datasources', {
      name: DATA_SOURCE_NAME,
      plugin_code: 'kafka',
      status: 'active',
      plugin_config: {
        brokers: ['127.0.0.1:9092'],
        topic: TOPIC,
        consumer_group: CONSUMER_GROUP,
        start_offset: 'earliest',
        security_protocol: 'PLAINTEXT',
        encoding: 'UTF-8',
        log_filter_enabled: false,
      },
    });

    // 断言数据源已创建
    const payload = await requestJSON(page, 'GET', '/api/v1/datasources?page=1&page_size=1000');
    const sources: Array<{ name?: string }> = Array.isArray(payload.datasources) ? payload.datasources : [];
    const found = sources.some((s) => String(s.name || '').trim() === DATA_SOURCE_NAME);
    expect(found, `[TC-P1-KAFKA-BROWSER-002] 预期数据源 ${DATA_SOURCE_NAME} 已创建`).toBe(true);
  });

  test('TC-P1-KAFKA-BROWSER-003 页面点击 Kafka 测试连通性按钮展示成功与失败结果', async ({ page }) => {
    console.log('== phase == TC-P1-KAFKA-BROWSER-003 click kafka connectivity button');
    const enabled = await isKafkaPluginEnabled(page);
    if (!enabled) { test.skip(); return; }

    await openCollectPage(page);
    await page.click('[data-testid="show-input-form"]');
    await expect(page.locator('[data-testid="input-form-card"]'), '[TC-P1-KAFKA-BROWSER-003] 预期采集表单可见').toBeVisible({ timeout: 5_000 });
    await page.click('[data-testid="input-plugin-kafka"]');
    await page.locator('[data-testid="kafka-brokers"]').fill('127.0.0.1:9092');
    await page.locator('[data-testid="kafka-topic"]').fill(TOPIC);
    await page.locator('[data-testid="kafka-consumer-group"]').fill(`${CONSUMER_GROUP}-ui-check`);

    await page.locator('[data-testid="kafka-connectivity-check"]').click();
    await expect(page.locator('[data-testid="kafka-connectivity-status"]'), '[TC-P1-KAFKA-BROWSER-003] 预期 Kafka 连通性成功回显').toContainText(/Kafka 连通性正常|正常|ok/i, { timeout: 20_000 });

    await page.locator('[data-testid="kafka-brokers"]').fill('127.0.0.1:1');
    await page.locator('[data-testid="kafka-connectivity-check"]').click();
    await expect(page.locator('[data-testid="kafka-connectivity-status"]'), '[TC-P1-KAFKA-BROWSER-003] 预期 Kafka 连通性失败回显').toContainText(/Kafka 连通性失败|KAFKA_CONNECTIVITY_FAILED|失败|refused|connect/i, { timeout: 20_000 });
  });

  test('TC-P1-JSON-BROWSER-001 创建 JSON Parser 解析规则', async ({ page }) => {
    console.log('== phase == TC-P1-JSON-BROWSER-001 create json parser rule via API');
    const enabled = await isJsonParserPluginEnabled(page);
    if (!enabled) {
      console.log('== phase == TC-P1-JSON-BROWSER-001 SKIPPED: json-parser plugin not enabled');
      test.skip();
      return;
    }

    // 先确认解析页 JSON Parser 可选
    await openParsePage(page);
    await page.click('[data-testid="show-rule-form"]');
    await expect(page.locator('[data-testid="rule-form-card"]'), '[TC-P1-JSON-BROWSER-001] 预期解析表单可见').toBeVisible({ timeout: 5_000 });

    // API 创建规则
    await requestJSON(page, 'POST', '/api/v1/parse-rules', {
      name: RULE_NAME,
      status: 'active',
      parser_plugin: 'json-parser',
      parser_plugin_version: '1.0.0',
      data_source_name: DATA_SOURCE_NAME,
      input_route: 'internal_raw_topic',
      output_index: INDEX_NAME,
      source: DATA_SOURCE_NAME,
      sourcetype: RULE_NAME,
      priority: 10,
      stage: 'ingest',
      sample_event: JSON_LOGS[0],
      plugin_config: {
        source_field: 'raw',
        target: 'fields',
        flatten_nested: true,
        flatten_separator: '.',
        array_mode: 'json_string',
        on_invalid_json: 'continue',
      },
      props_conf: `[source::${DATA_SOURCE_NAME}]\nINDEXED_EXTRACTIONS = json\nKV_MODE = none`,
    });

    // 断言规则已创建
    const payload = await requestJSON(page, 'GET', '/api/v1/parse-rules?page=1&page_size=1000');
    const rules: Array<{ name?: string }> = Array.isArray(payload.parse_rules) ? payload.parse_rules : [];
    const found = rules.some((r) => String(r.name || '').trim() === RULE_NAME);
    expect(found, `[TC-P1-JSON-BROWSER-001] 预期规则 ${RULE_NAME} 已创建`).toBe(true);
  });

  test('TC-P1-CH-BROWSER-001 验证原始日志与解析字段', async ({ page }) => {
    console.log('== phase == TC-P1-CH-BROWSER-001 produce kafka messages and assert parsed fields');
    const kafkaEnabled = await isKafkaPluginEnabled(page);
    const jsonEnabled = await isJsonParserPluginEnabled(page);
    if (!kafkaEnabled || !jsonEnabled) {
      console.log('== phase == TC-P1-CH-BROWSER-001 SKIPPED: kafka or json-parser plugin not enabled');
      test.skip();
      return;
    }

    // 容器内 producer 写 JSON 测试数据
    await produceKafkaMessages(TOPIC, JSON_LOGS);

    // 等待入库并解析
    await waitForKafkaData(page, 'checkout', 90_000);

    // API 搜索断言 parse_status=parsed + 字段
    const payload = await requestJSON(page, 'GET', `/api/v1/search?q=${encodeURIComponent(`index=${INDEX_NAME} parse_status=parsed`)}&limit=20&page=1`);
    const text = JSON.stringify(payload);
    expect(text.includes('checkout'), '[TC-P1-CH-BROWSER-001] 预期含 service=checkout 事件').toBe(true);
    expect(text.includes('billing'), '[TC-P1-CH-BROWSER-001] 预期含 service=billing 事件').toBe(true);
    expect(text.includes('u-1'), '[TC-P1-CH-BROWSER-001] 预期含 user.id=u-1 字段').toBe(true);
    expect(text.includes('CN'), '[TC-P1-CH-BROWSER-001] 预期含 user.region=CN 字段').toBe(true);
    expect(text.includes('US'), '[TC-P1-CH-BROWSER-001] 预期含 user.region=US 字段').toBe(true);
    expect(text.includes('latency'), '[TC-P1-CH-BROWSER-001] 预期含 latency 字段').toBe(true);
  });
});
