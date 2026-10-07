import { useState } from 'react'
import type { FormEvent } from 'react'
import { ApiError, reserveStock } from '../api/client'
import type { TrackedReservation } from '../types'
import Alert from './Alert'

type Props = {
  itemId: string
  onReserved: (reservation: TrackedReservation) => void
  onAttempt: () => void
}

export default function ReserveForm({ itemId, onReserved, onAttempt }: Props) {
  const [userId, setUserId] = useState('')
  const [quantity, setQuantity] = useState('1')
  const [failure, setFailure] = useState<Error | null>(null)
  const [pending, setPending] = useState(false)

  const parsed = Number(quantity)
  const invalid = !Number.isInteger(parsed) || parsed < 1

  async function submit(e: FormEvent) {
    e.preventDefault()
    setFailure(null)

    if (!userId.trim()) {
      setFailure(
        new ApiError('the request was rejected before it reached the database', 422, {
          code: 'INVALID_INPUT',
          details: { field: 'user_id', reason: 'is required' },
        }),
      )
      return
    }
    if (invalid) {
      setFailure(
        new ApiError('the request was rejected before it reached the database', 422, {
          code: 'INVALID_INPUT',
          details: { field: 'quantity', reason: 'must be a whole number of at least 1' },
        }),
      )
      return
    }

    setPending(true)
    try {
      const res = await reserveStock(itemId, userId.trim(), parsed)
      onReserved({ ...res, user_id: userId.trim(), status: 'active' })
      onAttempt()
    } catch (err) {
      setFailure(err instanceof Error ? err : new Error('unknown failure'))
      onAttempt()
    } finally {
      setPending(false)
    }
  }

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>Reserve</h2>
        <span className="muted">{itemId}</span>
      </div>

      <form className="stack" onSubmit={(e) => void submit(e)}>
        <label>
          User id
          <input
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            placeholder="user_01"
          />
        </label>
        <label>
          Quantity
          <input
            value={quantity}
            inputMode="numeric"
            onChange={(e) => setQuantity(e.target.value)}
            className={invalid ? 'invalid' : ''}
          />
        </label>
        <button type="submit" disabled={pending}>
          {pending ? 'Reserving…' : 'Hold stock'}
        </button>
      </form>

      {failure && <Alert error={failure} />}
    </section>
  )
}
