<template>
  <div class="h-screen flex flex-col bg-dt-bg-secondary font-sans text-dt-text-primary relative">
    <!-- Tab Bar -->
    <div class="flex items-end justify-between bg-dt-bg-secondary border-b border-dt-border-dark">
      <div class="flex items-end">
        <div @click="selectTab( 'welcome' )"
          class="px-3 py-2 cursor-pointer text-xs border-r border-dt-border-light relative"
          :class="{
            'bg-dt-bg-primary text-dt-text-primary': activeTab === 'welcome',
            'bg-dt-bg-secondary text-dt-text-secondary hover:bg-dt-bg-hover': activeTab !== 'welcome',
          }"
        >
          <img :src="isDark ? logoSmallWhite : logoSmallBlack" class="w-4"/>
          <div
            v-if="activeTab === 'welcome'"
            class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent"
          ></div>
        </div>
        <div
          v-for="tab in tabs"
          :key="tab.id"
          @click="selectTab(tab.id)"
          class="px-4 py-2 cursor-pointer text-xs border-r border-dt-border-light relative"
          :class="{
            'bg-dt-bg-primary text-dt-text-primary': activeTab === tab.id,
            'bg-dt-bg-secondary text-dt-text-secondary hover:bg-dt-bg-hover': activeTab !== tab.id,
          }"
        >
          {{ tab.label }}
          <!-- Active tab indicator -->
          <div
            v-if="activeTab === tab.id"
            class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent"
          ></div>
        </div>
      </div>

      <!-- Settings -->
      <div class="flex items-center gap-1 px-2">
        <ThemeToggle />
        <div class="relative">
          <button
            @click="selectTab('settings')"
            class="p-1 rounded hover:bg-dt-bg-hover dark:hover:bg-dt-bg-dark-hover text-dt-text-secondary dark:text-dt-text-dark-secondary cursor-pointer"
            :class="{ 'bg-dt-bg-hover dark:bg-dt-bg-dark-hover': activeTab === 'settings' }"
          >
            <AdjustmentsIcon class="w-4 h-4" />
          </button>
          <!-- Active tab indicator -->
          <div
            v-if="activeTab === 'settings'"
            class="absolute bottom-0 left-0 right-0 h-0.5 bg-dt-accent"
          ></div>
        </div>
      </div>
    </div>

    <!-- Panel Content -->
    <div class="flex-1 overflow-hidden bg-dt-bg-primary dark:bg-dt-bg-dark-primary">
      <Requests v-if="activeTab === 'requests'" />
      <Connections v-else-if="activeTab === 'connections'" />
      <Processes v-else-if="activeTab === 'processes'" />
      <Welcome v-else-if="activeTab === 'welcome'" :connectionStatus="status" @selectTab="selectTab" />
      <Settings v-else-if="activeTab === 'settings'" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useUrlParams } from '@/composables/urlParams'
import { useEvents } from '@/composables/events'
import { useTheme } from '@/composables/theme'
import { useHttpStore } from '@/stores/http'
import { useConnectionsStore } from '@/stores/connections'
import { useProcessesStore } from '@/stores/processes'
import Requests from '@/components/pages/Requests.vue'
import Connections from '@/components/pages/Connections.vue'
import Processes from '@/components/pages/Processes.vue'
import Welcome from '@/components/pages/Welcome.vue'
import Settings from '@/components/pages/Settings.vue'
import ThemeToggle from '@/components/ux/ThemeToggle.vue'
import AdjustmentsIcon from '@/components/icons/AdjustmentsIcon.vue'
import logoSmallBlack from '@/assets/logo-small-black.svg'
import logoSmallWhite from '@/assets/logo-small-white.svg'

// Theme management
const { isDark } = useTheme()

// Restore all buffers from sessionStorage on app mount
const httpStore = useHttpStore()
const connectionsStore = useConnectionsStore()
const processesStore = useProcessesStore()

onMounted(() => {
  httpStore.restoreFromStorage()
  connectionsStore.restoreFromStorage()
  processesStore.restoreFromStorage()
  
  // Start periodic persistence for all stores
  httpStore.startPeriodicPersistence()
  connectionsStore.startPeriodicPersistence()
  processesStore.startPeriodicPersistence()
})

const params = useUrlParams()

const tabs: { id: string; label: string }[] = [
  { id: 'processes', label: 'Processes' },
  { id: 'connections', label: 'Connections' },
  { id: 'requests', label: 'Requests' },
]

// get active tab from URL, default to 'requests'
const activeTab = computed(() => params.tab || 'welcome' as string)

// change the active tab
const selectTab = (tabId: string) => {
  params.tab = tabId
  // Smart clearing: only clear params that don't belong to target tab
  if (tabId !== 'requests') delete params.request_id
  if (tabId !== 'connections') delete params.connection_id
  if (tabId !== 'processes') delete params.process_id
}

// connect to the SSE endpoint
const { status } = useEvents()
</script>
