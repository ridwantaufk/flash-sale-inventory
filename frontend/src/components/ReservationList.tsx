import { useEffect, useState } from 'react'
import { ApiError, confirmReservation } from '../api/client'
import { mmss, remaining } from '../hooks/useNow'
import type { TrackedReservation } from '../types'
import Alert from './Alert'

type Props = {
  reservations: TrackedReservation[]
  now: number
  onChange: (next: TrackedReservation[]) => void
  onSettled: () => void
}

export default function ReservationList({ reservations, now, onChange, onSettled }: Props) {
  const [busy, setBusy] = useState('')
  const [failure, setFailure] = useState<Error | null>(null)

  useEffect(() => {
    const stillActive = reservations.filter(
      (r) => r.status === 'active' && remaining(r.expires_at, now) === 0,
    )
    if (stillActive.length === 0) return
    onChange(
      reservations.map((r) =>
        stillActive.some((s) => s.reservation_id === r.reservation_id)
          ? { ...r, status: 'expired' as const }
          : r,
      ),
    )
  }, [reservations, now, onChange])

  async function confirm(target: TrackedReservation) {
    setBusy(target.reservation_id)
    setFailure(null)
    try {
      const res = await confirmReservation(target.reservation_id)
      onChange(
        reservations.map((r) =>
          r.reservation_id === target.reservation_id
            ? { ...r, status: 'confirmed' as const, confirmed_at: res.confirmed_at }
            : r,
        ),
      )
      onSettled()
    } catch (err) {
      setFailure(err instanceof Error ? err : new Error('unknown failure'))
      if (err instanceof ApiError && err.code === 'RESERVATION_EXPIRED') {
        onChange(
          reservations.map((r) =>
            r.reservation_id === target.reservation_id ? { ...r, status: 'expired' as const } : r,
          ),
        )
      }
      onSettled()
    } finally {
      setBusy('')
    }
  }

  if (reservations.length === 0) {
    return (
      <section className="panel">
        <div className="panel-head">
          <h2>My reservations</h2>
        </div>
        <p className="muted">Nothing held yet. Reserve something and it appears here with a countdown.</p>
      </section>
    )
  }

  return (
    <section className="panel wide">
      <div className="panel-head">
        <h2>My reservations</h2>
        <span className="muted">{reservations.filter((r) => r.status === 'active').length} active</span>
      </div>

      {failure && <Alert error={failure} />}

      <ul className="reservations">
        {[...reservations].reverse().map((r) => {
          const left = remaining(r.expires_at, now)
          return (
            <li key={r.reservation_id} className={`reservation ${r.status}`}>
              <div className="reservation-id">
                <strong>{r.reservation_id}</strong>
                <span className="muted">
                  {r.item_id} · {r.quantity} unit{r.quantity === 1 ? '' : 's'} · {r.user_id}
                </span>
              </div>

              <div className="reservation-state">
                {r.status === 'active' && (
                  <span className={left <= 10 ? 'countdown urgent' : 'countdown'}>{mmss(left)}</span>
                )}
                {r.status !== 'active' && <span className={`badge ${r.status}`}>{r.status}</span>}

                {r.status === 'active' ? (
                  <button type="button" disabled={busy === r.reservation_id} onClick={() => void confirm(r)}>
                    {busy === r.reservation_id ? 'Confirming…' : 'Confirm purchase'}
                  </button>
                ) : (
                  <span className="muted">
                    {r.status === 'confirmed'
                      ? `paid ${new Date(r.confirmed_at ?? r.expires_at).toLocaleTimeString()}`
                      : 'returned to stock'}
                  </span>
                )}
              </div>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
