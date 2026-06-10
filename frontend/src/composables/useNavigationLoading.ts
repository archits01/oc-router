/**
 * 导航加载Status组合式函数
 * 管理路由切换时的加载Status，支持防闪烁逻辑
 */
import { ref, readonly, computed } from 'vue'

/**
 * 导航加载Status管理
 *
 * 功能：
 * 1. 在路由切换时显示加载Status
 * 2. 快速导航（< 100ms）不显示加载指示器（防闪烁）
 * 3. 导航Cancel时正确ResetStatus
 */
export function useNavigationLoading() {
  const _isLoading = ref(false)

  let navigationStartTime: number | null = null

  let showLoadingTimer: ReturnType<typeof setTimeout> | null = null

  const shouldShowLoading = ref(false)

  const ANTI_FLICKER_DELAY = 100

  /**
   * 清理计时器
   */
  const clearTimer = (): void => {
    if (showLoadingTimer !== null) {
      clearTimeout(showLoadingTimer)
      showLoadingTimer = null
    }
  }

  /**
   * 导航开始时调用
   */
  const startNavigation = (): void => {
    navigationStartTime = Date.now()
    _isLoading.value = true

    clearTimer()
    showLoadingTimer = setTimeout(() => {
      if (_isLoading.value) {
        shouldShowLoading.value = true
      }
    }, ANTI_FLICKER_DELAY)
  }

  /**
   * 导航结束时调用
   */
  const endNavigation = (): void => {
    clearTimer()
    _isLoading.value = false
    shouldShowLoading.value = false
    navigationStartTime = null
  }

  /**
   * 导航Cancel时调用（比如快速连续点击不同链接）
   */
  const cancelNavigation = (): void => {
    clearTimer()
    navigationStartTime = null
  }

  /**
   * Reset所有Status（用于测试）
   */
  const resetState = (): void => {
    clearTimer()
    _isLoading.value = false
    shouldShowLoading.value = false
    navigationStartTime = null
  }

  /**
   * 获取导航持续时间（毫秒）
   */
  const getNavigationDuration = (): number | null => {
    if (navigationStartTime === null) {
      return null
    }
    return Date.now() - navigationStartTime
  }

  const isLoading = computed(() => shouldShowLoading.value)

  const isNavigating = readonly(_isLoading)

  return {
    isLoading,
    isNavigating,
    startNavigation,
    endNavigation,
    cancelNavigation,
    resetState,
    getNavigationDuration,
    ANTI_FLICKER_DELAY
  }
}

let navigationLoadingInstance: ReturnType<typeof useNavigationLoading> | null = null

export function useNavigationLoadingState() {
  if (!navigationLoadingInstance) {
    navigationLoadingInstance = useNavigationLoading()
  }
  return navigationLoadingInstance
}

export function _resetNavigationLoadingInstance(): void {
  if (navigationLoadingInstance) {
    navigationLoadingInstance.resetState()
  }
  navigationLoadingInstance = null
}
