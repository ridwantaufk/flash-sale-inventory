import type { Confirmed, Reservation, Stock } from '../types'

const BASE = '/api/v1/inventory'

type ErrorBody = {
  code?: string
  message?: string
  request_id?: string
  details?: { field?: string; reason?: string }
}

export class ApiError extends Error {
  code: string
  status: number
  field?: string
  reason?: string
  requestId?: string

  constructor(message: string, status: number, body: ErrorBody) {
    super(message)
    this.name = 'ApiError'
    this.code = body.code ?? 'UNKNOWN'
    this.status = status
    this.field = body.details?.field
    this.reason = body.details?.reason
    this.requestId = body.request_id
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(path, {
      headers: { 'Content-Type': 'application/json' },
      ...init,
    })
  } catch {
    throw new ApiError('the stock service is not reachable', 0, { code: 'NETWORK' })
  }

  const body: ErrorBody | null = await res.json().catch(() => null)
  if (!res.ok) {
    const detail = body ?? {}
    throw new ApiError(detail.message ?? `request failed (${res.status})`, res.status, detail)
  }
  if (!body) {
    throw new ApiError('the stock service returned an empty body', res.status, { code: 'EMPTY' })
  }
  return body as unknown as T
}

export function fetchStock(itemId: string): Promise<Stock> {
  return call<Stock>(`${BASE}/stock?item_id=${encodeURIComponent(itemId)}`)
}

export function reserveStock(itemId: string, userId: string, quantity: number): Promise<Reservation> {
  return call<Reservation>(`${BASE}/reserve`, {
    method: 'POST',
    body: JSON.stringify({ item_id: itemId, user_id: userId, quantity }),
  })
}

export function confirmReservation(reservationId: string): Promise<Confirmed> {
  return call<Confirmed>(`${BASE}/confirm`, {
    method: 'POST',
    body: JSON.stringify({ reservation_id: reservationId }),
  })
}
