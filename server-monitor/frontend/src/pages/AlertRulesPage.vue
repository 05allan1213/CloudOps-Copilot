<script setup lang="ts">
import { onMounted, reactive, ref, computed } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import type { FormInstance, FormRules } from "element-plus";

import {
  createAlertRule,
  deleteAlertRule,
  fetchAlertRules,
  syncAlertRules,
  updateAlertRule,
  type AlertRuleRequest,
} from "../api/alertRules";
import type { AlertRule, AlertRuleSyncResult } from "../types";
import { severityTagType } from "../composables/useTagTypes";
import PageHeader from "../components/common/PageHeader.vue";
import StateWrapper from "../components/common/StateWrapper.vue";

const emptyForm: AlertRuleRequest = {
  name: "",
  expr: "",
  duration: "2m",
  severity: "warning",
  summary: "",
  description: "",
  enabled: true,
};

const rules = ref<AlertRule[]>([]);
const loading = ref(false);
const saving = ref(false);
const syncing = ref(false);
const editingID = ref<number | null>(null);
const syncResult = ref<AlertRuleSyncResult | null>(null);
const formRef = ref<FormInstance>();
const form = reactive<AlertRuleRequest>({ ...emptyForm });

const stateKey = computed(() => {
  if (loading.value) return "loading";
  if (rules.value.length === 0) return "empty";
  return "default";
});

const formRules: FormRules = {
  name: [{ required: true, message: "请输入规则名称", trigger: "blur" }],
  expr: [{ required: true, message: "请输入 PromQL 表达式", trigger: "blur" }],
  duration: [{ required: true, message: "请输入持续时间", trigger: "blur" }],
};

function resetForm() {
  Object.assign(form, emptyForm);
  editingID.value = null;
  formRef.value?.resetFields();
}

function editRule(rule: AlertRule) {
  editingID.value = rule.id;
  Object.assign(form, {
    name: rule.name,
    expr: rule.expr,
    duration: rule.duration,
    severity: rule.severity,
    summary: rule.summary,
    description: rule.description,
    enabled: rule.enabled,
  });
}

async function loadRules() {
  loading.value = true;
  try {
    rules.value = await fetchAlertRules();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "加载告警规则失败");
  } finally {
    loading.value = false;
  }
}

async function saveRule() {
  const valid = await formRef.value?.validate().catch(() => false);
  if (!valid) return;

  saving.value = true;
  try {
    if (editingID.value) {
      await updateAlertRule(editingID.value, form);
      ElMessage.success("告警规则已更新");
    } else {
      await createAlertRule(form);
      ElMessage.success("告警规则已创建");
    }
    resetForm();
    await loadRules();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "保存告警规则失败");
  } finally {
    saving.value = false;
  }
}

async function removeRule(rule: AlertRule) {
  try {
    await ElMessageBox.confirm(`确认删除告警规则「${rule.name}」？`, "删除确认", {
      confirmButtonText: "删除",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  try {
    await deleteAlertRule(rule.id);
    ElMessage.success("告警规则已删除");
    await loadRules();
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "删除告警规则失败");
  }
}

async function syncRules() {
  syncing.value = true;
  syncResult.value = null;
  try {
    syncResult.value = await syncAlertRules();
    ElMessage.success("告警规则已同步到 Prometheus");
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : "同步告警规则失败");
  } finally {
    syncing.value = false;
  }
}

onMounted(loadRules);
</script>

<template>
  <section class="alert-rules-page">
    <PageHeader
      title="告警规则"
      subtitle="保存到 MySQL 后，可手动同步到 Prometheus rules 文件"
    >
      <el-button
        type="primary"
        :loading="syncing"
        @click="syncRules"
      >
        {{ syncing ? "同步中" : "同步规则" }}
      </el-button>
    </PageHeader>

    <el-card
      shadow="never"
      class="form-card"
    >
      <template #header>
        <span class="card-title">{{ editingID ? "编辑规则" : "创建规则" }}</span>
      </template>
      <el-form
        ref="formRef"
        :model="form"
        :rules="formRules"
        label-width="90px"
        label-position="top"
        @submit.prevent="saveRule"
      >
        <el-row :gutter="16">
          <el-col
            :xs="24"
            :sm="12"
            :md="8"
          >
            <el-form-item
              label="名称"
              prop="name"
            >
              <el-input
                v-model.trim="form.name"
                maxlength="128"
                placeholder="规则名称"
              />
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
            :md="4"
          >
            <el-form-item
              label="持续时间"
              prop="duration"
            >
              <el-input
                v-model.trim="form.duration"
                placeholder="2m"
              />
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
            :md="4"
          >
            <el-form-item
              label="级别"
              prop="severity"
            >
              <el-select
                v-model="form.severity"
                style="width: 100%"
              >
                <el-option
                  label="critical"
                  value="critical"
                />
                <el-option
                  label="warning"
                  value="warning"
                />
                <el-option
                  label="info"
                  value="info"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
            :md="4"
          >
            <el-form-item label="启用">
              <el-switch v-model="form.enabled" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item
          label="PromQL"
          prop="expr"
        >
          <el-input
            v-model.trim="form.expr"
            type="textarea"
            :rows="3"
            placeholder="PromQL 表达式"
          />
        </el-form-item>
        <el-row :gutter="16">
          <el-col
            :xs="24"
            :sm="12"
          >
            <el-form-item label="摘要">
              <el-input
                v-model.trim="form.summary"
                maxlength="512"
                placeholder="告警摘要"
              />
            </el-form-item>
          </el-col>
          <el-col
            :xs="24"
            :sm="12"
          >
            <el-form-item label="描述">
              <el-input
                v-model.trim="form.description"
                type="textarea"
                :rows="2"
                placeholder="告警描述"
              />
            </el-form-item>
          </el-col>
        </el-row>
        <el-form-item>
          <el-button
            type="primary"
            :loading="saving"
            @click="saveRule"
          >
            {{ saving ? "保存中" : editingID ? "更新规则" : "创建规则" }}
          </el-button>
          <el-button @click="resetForm">
            清空
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-alert
      v-if="syncResult"
      type="success"
      show-icon
      closable
      style="margin-bottom: 16px"
    >
      <template #title>
        同步结果
      </template>
      <template #default>
        校验 {{ syncResult.validated ? "通过" : "未通过" }}，
        规则数 {{ syncResult.rule_count }}，
        Reload {{ syncResult.reloaded ? "成功" : "未执行" }}
      </template>
    </el-alert>

    <StateWrapper
      :state="stateKey"
      empty-text="暂无告警规则"
    >
      <template #retry>
        <el-button
          type="primary"
          @click="loadRules"
        >
          重试
        </el-button>
      </template>
      <el-card shadow="never">
        <el-table
          :data="rules"
          stripe
          style="width: 100%"
        >
          <el-table-column
            prop="name"
            label="名称"
            min-width="140"
          />
          <el-table-column
            label="级别"
            width="100"
            align="center"
          >
            <template #default="{ row }">
              <el-tag
                :type="severityTagType(row.severity)"
                size="small"
                effect="plain"
              >
                {{ row.severity }}
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
            prop="expr"
            label="表达式"
            min-width="200"
          >
            <template #default="{ row }">
              <code class="expr-cell">{{ row.expr }}</code>
            </template>
          </el-table-column>
          <el-table-column
            label="操作"
            width="160"
            align="center"
          >
            <template #default="{ row }">
              <el-button
                size="small"
                text
                type="primary"
                @click="editRule(row)"
              >
                编辑
              </el-button>
              <el-button
                size="small"
                text
                type="danger"
                @click="removeRule(row)"
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
.alert-rules-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.form-card :deep(.el-card__body) {
  padding: 20px;
}

.expr-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
  color: var(--el-text-color-secondary);
}
</style>
