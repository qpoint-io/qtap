<template>
  <div class="flex w-screen h-screen items-center justify-center">
    <div class="flex gap-10">
      <TrafficGraphic />

      <!-- Info -->
      <div>
        <div class="flex gap-5 items-center border-bottom border-b border-b-gray-300 dark:border-b-gray-600 pb-8 mb-8">
          <img :src="isDark ? logoWhiteSvg : logoBlackSvg" class="w-8 -mb-2"/>
          <div class="text-[30px] font-bold text-dt-text-primary dark:text-[#b58bf0] ">Qpoint DevTools</div>
          <UxStatusPill :status="connectionStatus" class="ml-8"/>
        </div>
        <div class="flex gap-3">
          <UxButton @click="selectTab('processes')">View Processes</UxButton>
          <UxButton @click="selectTab('connections')">View Connections</UxButton>
          <UxButton @click="selectTab('requests')">View Requests</UxButton>
        </div>
      </div>
    </div>

    <!-- Footer -->
    <div class="flex justify-between absolute bottom-0 left-0 right-0 px-8 py-4 text-[#9b5cf2] dark:text-[#b794f6]">
      <div class="font-bold text-18 flex items-center gap-2">Qpoint <img :src="isDark ? heartWhiteSvg : heartSvg" class="w-3 "/> Opensource</div>
      <div class="flex gap-8">
        <a href="http://github.com/qpoint-io"><div class="text-[#9b5cf2] dark:text-[#b794f6] hover:text-black dark:hover:text-white flex items-center gap-2 font-bold "><img :src="isDark ? githubLightSvg : githubSvg" class="w-[24px] "/> Github</div></a>
        <a href="http://qpoint.io"><div class="text-[#9b5cf2] dark:text-[#b794f6] hover:text-black dark:hover:text-white flex items-center gap-2 font-bold "><img :src="isDark ? logoWhiteSvg : logoBlackSvg" class="w-[17px] "/> Qpoint.io</div></a>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useTheme } from '@/composables/theme'
import TrafficGraphic from '@/components/pages/welcome/TrafficGraphic.vue';
import UxButton from '@/components/ux/Button.vue'
import UxStatusPill from '@/components/ux/StatusPill.vue'
import logoBlackSvg from '@/assets/logo-mark-black.svg'
import logoWhiteSvg from '@/assets/logo-mark-white.svg'
import heartSvg from '@/assets/heart.svg'
import heartWhiteSvg from '@/assets/heart-white.svg'
import githubSvg from '@/assets/github.svg'
import githubLightSvg from '@/assets/github-light.svg'

// Theme management
const { isDark } = useTheme()

// define props
interface Props {
  connectionStatus: 'CONNECTING' | 'OPEN' | 'CLOSED'
}

// default props
withDefaults(defineProps<Props>(), {
  connectionStatus: 'CLOSED',
})

// define emits
const emit = defineEmits(['selectTab'])

const selectTab = (tabId: string) => {
  emit('selectTab', tabId)
}
</script>
