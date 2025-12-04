<template>
  <div class="flex items-center">
    <template v-if="direction === 'ingress'">
      <IngressIcon class="text-blue-600 dark:text-blue-400" :class="{ 'mr-1.5': showLabel }" />
      <span v-if="showLabel" class="text-[11px] text-blue-600 dark:text-blue-400">ingress</span>
    </template>
    <template v-else>
      <EgressIcon class="text-purple-600 dark:text-purple-400" :class="{ 'mr-1.5': showLabel }" />
      <span v-if="showLabel" class="text-[11px] text-purple-600 dark:text-purple-400">egress</span>
    </template>
  </div>
</template>

<script setup lang="ts">
import IngressIcon from '@/components/icons/IngressIcon.vue'
import EgressIcon from '@/components/icons/EgressIcon.vue'

interface Props {
  direction?: string
  showLabel?: boolean
}

withDefaults(defineProps<Props>(), {
  direction: 'egress',
  showLabel: true
})
</script>

