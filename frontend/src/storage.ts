import type { TrackedReservation } from './types'

const KEY = 'flash-sale.reservations'

export function loadReservations(): TrackedReservation[] {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return []
    const parsed: unknown = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as TrackedReservation[]) : []
  } catch {
    return []
  }
}

export function saveReservations(items: TrackedReservation[]): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(items))
  } catch {
  }
}
