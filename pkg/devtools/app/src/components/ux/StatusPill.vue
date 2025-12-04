<template>
    <div 
      class="relative flex items-center gap-2 ml-auto font-bold rounded-full px-8 py-2 text-[18px]"
      :class="connectionClass"
    >
      <img v-if="status === 'OPEN'" :src="checkIcon" class="w-4 "/>
      <div v-if="status === 'CLOSED'"  class="">
        <div class="pr-4">
          <div class="w-[34px] absolute top-[5px] left-3 text-[#121212] dark:text-gray-300">
            <SleeperIcon />
          </div>
        </div>
      </div>
      <div v-if="status === 'CONNECTING'"  class="">
        <div class="pr-4">
          <div class="w-[34px] absolute top-[8px] left-2">
            <img class="w-full spin-animation" :src="spinnerIcon" />
          </div>
        </div>
        
      </div>

      {{ connectionStatus }}
    </div>
</template>

<style>
@import "../../assets/main.css";

.status-open{
  @apply bg-[#edfffa] border border-[#2369ff];
  @apply dark:bg-black  dark:border-none text-[#a067f0];
}
.status-opening{
  @apply bg-[#f5f5f5] border border-[#2369ff];
  @apply dark:bg-black dark:border-[#a067f0];
}
.status-closed{
  @apply bg-[#f5f5f5] border border-dt-border-dark;
  @apply dark:bg-[#35363a] dark:border-[#494c50];
}

@keyframes spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

.spin-animation {
  animation: spin 5s linear infinite;
}
</style>

<script setup lang="ts">
import checkSvg from '@/assets/check.svg'
import whiteCheckSvg from '@/assets/check-white.svg'
import spinnerSvg from '@/assets/spinner.svg'
import whiteSpinnerSvg from '@/assets/spinner-white.svg'
import SleeperIcon from '@/components/icons/SleeperIcon.vue'
import { computed } from 'vue'
import { useTheme } from '@/composables/theme'

interface Props {
  status: 'CONNECTING' | 'OPEN' | 'CLOSED'
}

const props = withDefaults(defineProps<Props>(), {
  status: 'CLOSED',
})

// Theme detection
const { isDark } = useTheme()

// Computed icons
const checkIcon = computed(() => isDark.value ? whiteCheckSvg : checkSvg)
const spinnerIcon = computed(() => isDark.value ? whiteSpinnerSvg : spinnerSvg)

// Human readable status
const connectionStatus = computed<string>(()=>{
  if(props.status == 'OPEN')
    return 'Connected'
  if(props.status == 'CLOSED')
    return 'Connection Closed'
  if(props.status == 'CONNECTING')
    return 'Connecting..'

  return 'Connection Closed'
})

// Human readable status
const connectionClass = computed<string>(()=>{
  if(props.status == 'OPEN')
    return 'status-open'
  if(props.status == 'CLOSED')
    return 'status-closed'
  if(props.status == 'CONNECTING')
    return 'status-opening'

  return 'status-closed'
})


</script>
