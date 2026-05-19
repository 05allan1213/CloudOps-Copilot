<script setup lang="ts">
import { onMounted, reactive, ref, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import type { FormInstance, FormRules } from "element-plus";

import {
  createNotificationChannel,
  deleteNotificationChannel,
  fetchNotificationChannels,
  testNotificationChannel,
  updateNotificationChannel,
  type NotificationChannelRequest,
} from "../api/channels";
import type { NotificationChannel, NotificationChannelTestResult } from "../types";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const emptyForm: NotificationChannelRequest = {
  name: "",
  type: "webhook",
  url: "",
  enabled: true,
};

const channels = ref<NotificationChannel[]>([]);
const loading = ref(false);
const saving = ref(false);
const testingID = ref<number | null>(null);
const editingID = ref<number | null>(null);
const testResult = ref<NotificationChannelTestResult | null>(null);
const formRef = ref<FormInstance>();
const form = reactive<NotificationChannelRequest>({ ...emptyForm });

const stateKey = computed(() => {
  if (loading.value) return "loading";
  if (channels.value.length === 0) return "empty";
  return "default";
});

const formRules: FormRules = {
  name: [{ required: true, message: "请输入渠道名称", trigger: "blur" }],
  url: [{ required: true, message: "请输入 Webhook URL", trigger: "blur" }],
};

function resetForm() {
  Object.assign(form, emptyForm);
  editingID.value = null;
  formRef.value?.resetFields();
}

function editChannel(channel: NotificationChannel) {
  editingID.value = channel.id;
  Object.assign(form, {
    name: channel.name,
    type: channel.type,
    url: channel.url,
    enabled: channel.enabled,
  });
}

async function loadChannels() {
  loading.value = true;
  try {
    channels.value = await fetchNotificationChannels();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "加载通知渠道失败");
  } finally {
    loading.value = false;
  }
}

async function saveChannel() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  saving.value = true;
  testResult.value = null;
  try {
    if (editingID.value) {
      await updateNotificationChannel(editingID.value, form);
      ElMessage.success("通知渠道已更新");
    } else {
      await createNotificationChannel(form);
      ElMessage.success("通知渠道已创建");
    }
    resetForm();
    await loadChannels();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "保存通知渠道失败");
  } finally {
    saving.value = false;
  }
}

async function removeChannel(channel: NotificationChannel) {
  try {
    await ElMessageBox.confirm(`确认删除通知渠道「${channel.name}」？`, "删除确认", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  testResult.value = null;
  try {
    await deleteNotificationChannel(channel.id);
    ElMessage.success("通知渠道已删除");
    await loadChannels();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "删除通知渠道失败");
  }
}

async function testChannel(channel: NotificationChannel) {
  testingID.value = channel.id;
  testResult.value = null;
  try {
    testResult.value = await testNotificationChannel(channel.id);
    ElMessage.success("通知渠道连通性测试通过");
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "通知渠道测试失败");
  } finally {
    testingID.value = null;
  }
}

onMounted(loadChannels);
</script>

<template>
  <section class="channels-page">
    <PageHeader
      title="通知渠道"
      subtitle="当前阶段只维护 Webhook 配置和连通性测试，不发送真实告警通知"
    />

    <el-card
      shadow="never"
      class="form-card"
    >
      <template #header>
        <span class="card-title">{{ editingID ? "编辑渠道" : "创建渠道" }}</span>
      </template>
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-position="top"
        @submit.prevent="saveChannel"
      >
        <el-row :gutter="16">
          <el-col
            :xs="24"
            :sm="12"
            :md="6"
          >
            <el-form-item
              label="名称"
              prop="name"
            >
              <el-input
                v-model.trim="form.name"
                maxlength="128"
                placeholder="渠道名称"
              />
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
            :md="6"
          >
            <el-form-item label="类型">
              <el-select
                v-model="form.type"
                style="width: 100%"
              >
                <el-option
                  label="webhook"
                  value="webhook"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
            :md="6"
          >
            <el-form-item label="启用">
              <el-switch v-model="form.enabled" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item
          label="Webhook URL"
          prop="url"
        >
          <el-input
            v-model.trim="form.url"
            maxlength="512"
            placeholder="https://example.com/webhook"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            :loading="saving"
            @click="saveChannel"
          >
            {{ saving ? "保存中" : editingID ? "更新渠道" : "创建渠道" }}
          </el-button>
          <el-button @click="resetForm">
            清空
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-alert
      v-if="testResult"
      type="success"
      show-icon
      closable
      style="margin-bottom: 16px"
    >
      <template #title>
        测试结果
      </template>
      <template #default>
        HTTP {{ testResult.status_code ?? "-" }}，耗时 {{ testResult.latency_ms ?? 0 }}ms
      </template>
    </el-alert>

    <StateWrapper
      :state="stateKey"
      empty-text="暂无通知渠道"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadChannels"
        >
          重试
        </el-button>
      </template>
      <el-card shadow="never">
        <el-table
          :data="channels"
          stripe
          style="width: 100%"
        >
          <el-table-column
            prop="name"
            label="名称"
            min-width="140"
          />
          <el-table-column
            label="类型"
            width="110"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                size="small"
                effect="plain"
              >
                {{ row.type }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            label="状态"
            width="90"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="row.enabled ? 'success' : 'info'"
                size="small"
              >
                {{ row.enabled ? "启用" : "停用" }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column
            prop="url"
            label="URL"
            min-width="200"
          >
            <template #default="{ row }">
              <code class="url-cell">{{ row.url }}</code>
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            width="220"
            align="center"
          >
            <template #default="{ row }">
              <el-button
                size="small"
                text
                type="primary"
                @click="editChannel(row)"
              >
                编辑
              </el-button>
              <el-button
                size="small"
                text
                type="warning"
                :loading="testingID === row.id"
                @click="testChannel(row)"
              >
                {{ testingID === row.id ? "测试中" : "测试" }}
              </el-button>
              <el-button
                size="small"
                text
                type="danger"
                @click="removeChannel(row)"
              >
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>
    </StateWrapper>
  </section>
</template>

<style scoped>
.channels-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-card :deep(.el-card__body) {
  padding: 20px;
}

.url-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
  color: var(--el-text-color-secondary);
}
</style>
