<script setup lang="ts">
import { ref } from "vue";
import { ElMessage } from "element-plus";

import { submitDiagnosisFeedback } from "../../api/diagnosis";

const props = defineProps<{
  reportId: number;
  existingFeedback?: { rating: string; comment?: string };
}>();

const emit = defineEmits<{
  submitted: [];
}>();

const ratingValue = ref(props.existingFeedback
  ? props.existingFeedback.rating === "useful" ? 5 : 2
  : 0);
const feedbackComment = ref(props.existingFeedback?.comment || "");
const submitting = ref(false);
const submitted = ref(!!props.existingFeedback);

async function handleRateChange(value: number) {
  if (submitted.value) return;
  ratingValue.value = value;
}

async function submitFeedback() {
  if (ratingValue.value === 0 || submitting.value) return;

  const rating = ratingValue.value >= 3 ? "useful" : "not_useful";
  submitting.value = true;
  try {
    await submitDiagnosisFeedback(props.reportId, {
      rating: rating as "useful" | "not_useful",
      comment: feedbackComment.value || undefined,
    });
    submitted.value = true;
    ElMessage.success("感谢您的反馈！");
    emit("submitted");
  } catch {
    ElMessage.error("反馈提交失败，请稍后重试");
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <el-card
    shadow="never"
    class="feedback-card"
  >
    <div class="feedback-content">
      <span class="feedback-label">这份诊断报告对您有帮助吗？</span>
      <el-rate
        v-model="ratingValue"
        :disabled="submitted"
        @change="handleRateChange"
      />
      <div
        v-if="!submitted && ratingValue > 0"
        class="comment-area"
      >
        <el-input
          v-model="feedbackComment"
          type="textarea"
          maxlength="500"
          :rows="2"
          placeholder="请输入您的反馈（可选，最多 500 字符）"
          show-word-limit
        />
        <el-button
          type="primary"
          size="small"
          :loading="submitting"
          @click="submitFeedback"
        >
          提交反馈
        </el-button>
      </div>
      <el-tag
        v-if="submitted"
        type="success"
        size="small"
      >
        已提交反馈
      </el-tag>
    </div>
  </el-card>
</template>

<style scoped>
.feedback-card {
  margin-bottom: 0;
}

.feedback-content {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.feedback-label {
  color: var(--el-text-color-secondary);
  font-size: 14px;
  font-weight: 500;
}

.comment-area {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
}
</style>
