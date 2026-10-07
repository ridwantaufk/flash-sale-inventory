export type Stock = {
  item_id: string
  total_stock: number
  reserved_stock: number
  available_stock: number
}

export type Reservation = {
  reservation_id: string
  item_id: string
  quantity: number
  expires_at: string
}

export type TrackedReservation = Reservation & {
  user_id: string
  status: 'active' | 'confirmed' | 'expired'
  confirmed_at?: string
}

export type Confirmed = {
  reservation_id: string
  confirmed_at: string
}

export type ApiFailure = {
  code: string
  message: string
  field?: string
  reason?: string
}
