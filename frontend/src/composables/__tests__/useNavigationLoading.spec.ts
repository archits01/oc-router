/**
 * useNavigationLoading 组合式函数单元测试
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  useNavigationLoading,
  _resetNavigationLoadingInstance
} from '../useNavigationLoading'

describe('useNavigationLoading', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    _resetNavigationLoadingInstance()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  describe('startNavigation', () => {
    it('导航开始时 isNavigating 应变为 true', () => {
      const { isNavigating, startNavigation } = useNavigationLoading()

      expect(isNavigating.value).toBe(false)

      startNavigation()

      expect(isNavigating.value).toBe(true)
    })

    it('导航开始后延迟显示加载指示器（防闪烁）', () => {
      const { isLoading, startNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()

      expect(isLoading.value).toBe(false)

      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)
    })
  })

  describe('endNavigation', () => {
    it('导航结束时 isLoading 应变为 false', () => {
      const { isLoading, startNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)

      endNavigation()
      expect(isLoading.value).toBe(false)
    })

    it('导航结束时 isNavigating 应变为 false', () => {
      const { isNavigating, startNavigation, endNavigation } = useNavigationLoading()

      startNavigation()
      expect(isNavigating.value).toBe(true)

      endNavigation()
      expect(isNavigating.value).toBe(false)
    })
  })

  describe('快速导航（< 100ms）防闪烁', () => {
    it('快速导航不应触发显示加载指示器', () => {
      const { isLoading, startNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()

      vi.advanceTimersByTime(ANTI_FLICKER_DELAY - 50)
      endNavigation()

      expect(isLoading.value).toBe(false)

      vi.advanceTimersByTime(100)
      expect(isLoading.value).toBe(false)
    })
  })

  describe('cancelNavigation', () => {
    it('导航Cancel时应正确ResetStatus', () => {
      const { isLoading, startNavigation, cancelNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY / 2)

      cancelNavigation()

      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(false)
    })
  })

  describe('getNavigationDuration', () => {
    it('应该Back正确的导航持续时间', () => {
      const { startNavigation, getNavigationDuration } = useNavigationLoading()

      expect(getNavigationDuration()).toBeNull()

      startNavigation()
      vi.advanceTimersByTime(500)

      const duration = getNavigationDuration()
      expect(duration).toBe(500)
    })

    it('导航结束后应Back null', () => {
      const { startNavigation, endNavigation, getNavigationDuration } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(500)
      endNavigation()

      expect(getNavigationDuration()).toBeNull()
    })
  })

  describe('resetState', () => {
    it('应该Reset所有Status', () => {
      const { isLoading, isNavigating, startNavigation, resetState, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)

      expect(isLoading.value).toBe(true)
      expect(isNavigating.value).toBe(true)

      resetState()

      expect(isLoading.value).toBe(false)
      expect(isNavigating.value).toBe(false)
    })
  })

  describe('连续导航场景', () => {
    it('连续快速导航应正确处理Status', () => {
      const { isLoading, startNavigation, cancelNavigation, endNavigation, ANTI_FLICKER_DELAY } = useNavigationLoading()

      startNavigation()
      vi.advanceTimersByTime(30)

      cancelNavigation()
      startNavigation()
      vi.advanceTimersByTime(30)

      cancelNavigation()
      startNavigation()

      vi.advanceTimersByTime(ANTI_FLICKER_DELAY)
      expect(isLoading.value).toBe(true)

      endNavigation()
      expect(isLoading.value).toBe(false)
    })
  })

  describe('ANTI_FLICKER_DELAY 常量', () => {
    it('应该为 100ms', () => {
      const { ANTI_FLICKER_DELAY } = useNavigationLoading()
      expect(ANTI_FLICKER_DELAY).toBe(100)
    })
  })
})
