<template>
  <section data-testid="not-found-page" class="tab-panel">
    <article class="card">
      <div class="card-head"><span>404 页面不存在</span><span class="status-line">{{ routePath }}</span></div>
      <p class="status-line">当前路径没有匹配的控制台模块，请通过顶部导航进入采集、解析、索引、搜索、插件管理或用户与权限页面。</p>
    </article>
  </section>
</template>

<script setup>
import { computed } from 'vue';
import { useRoute } from 'vue-router';
const route = useRoute();
const routePath = computed(() => route.fullPath || '/');
</script>
