import { useCallback, useEffect, useState } from 'react'
import { fetchStock } from '../api/client'
import type { Stock } from '../types'
import Alert from './Alert'

const POLL_MS = 5000

type Props = {
  itemId: string
  onItemIdChange: (itemId: string) => void
  refreshKey: number
}

export default function StockPanel({ itemId, onItemIdChange, refreshKey }: Props) {
  const [draft, setDraft] = useState(itemId)
  const [stock, setStock] = useState<Stock | null>(null)
  const [failure, setFailure] = useState<Error | null>(null)
  const [polling, setPolling] = useState(true)
  const [loadedAt, setLoadedAt] = useState('')

  const load = useCallback(async () => {
    try {
      const next = await fetchStock(itemId)
      setStock(next)
      setFailure(null)
      setLoadedAt(new Date().toLocaleTimeString())
    } catch (err) {
      setFailure(err instanceof Error ? err : new Error('unknown failure'))
    }
  }, [itemId])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  useEffect(() => {
    if (!polling) return
    const timer = setInterval(() => void load(), POLL_MS)
    return () => clearInterval(timer)
  }, [polling, load])

  return (
    <section className="panel">
      <div className="panel-head">
        <h2>Stock</h2>
        <div className="row">
          <button type="button" onClick={() => void load()}>
            Refresh
          </button>
          <button
            type="button"
            className={polling ? 'toggle on' : 'toggle'}
            aria-pressed={polling}
            onClick={() => setPolling((v) => !v)}
          >
            {polling ? 'polling 5s' : 'polling off'}
          </button>
        </div>
      </div>

      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault()
          const next = draft.trim()
          if (next) onItemIdChange(next)
        }}
      >
        <input
          aria-label="item id"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="item_4021"
        />
        <button type="submit">Look up</button>
      </form>

      {failure && <Alert error={failure} />}

      {stock && (
        <dl className="figures">
          <div>
            <dt>total</dt>
            <dd>{stock.total_stock}</dd>
          </div>
          <div>
            <dt>reserved</dt>
            <dd className="warn">{stock.reserved_stock}</dd>
          </div>
          <div>
            <dt>available</dt>
            <dd className="ok">{stock.available_stock}</dd>
          </div>
        </dl>
      )}

      <p className="note">
        {stock ? `${stock.item_id} as of ${loadedAt}` : 'no reading yet'}
        {' · '}the reserved figure settles within one expiry pass, so it can sit
        above the live total for a few seconds.
      </p>
    </section>
  )
}
