<template>
  <div class="flex h-full bg-dt-bg-primary">
    <!-- Left Nav -->
    <div class="w-48 border-r border-dt-border-light bg-dt-bg-secondary py-2">
      <h3 class="px-4 py-2 mt-2 pb-4 text-xs font-medium text-dt-text-secondary tracking-wide border-b border-dt-border-light">Settings</h3>
      <button
        v-for="category in categories"
        :key="category.id"
        @click="selectedCategory = category.id"
        class="w-full text-left px-4 py-2 text-xs cursor-pointer transition-colors"
        :class="selectedCategory === category.id
          ? 'bg-dt-bg-selected text-dt-text-primary'
          : 'text-dt-text-secondary hover:bg-dt-bg-hover hover:text-dt-text-primary'"
      >
        {{ category.label }}
      </button>
    </div>

    <!-- Right Content -->
    <div class="flex-1 overflow-auto p-6">
      <!-- Streaming -->
      <div v-if="selectedCategory === 'stream'">
        <Streaming />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import Streaming from '@/components/pages/settings/Streaming.vue'

const categories = [
  { id: 'stream', label: 'Streaming' },
]

const selectedCategory = ref('stream')
</script>
