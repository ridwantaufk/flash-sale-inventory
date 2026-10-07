import { useCallback, useEffect, useState } from 'react'
import ReservationList from './components/ReservationList'
import ReserveForm from './components/ReserveForm'
import StockPanel from './components/StockPanel'
import useNow from './hooks/useNow'
import { loadReservations, saveReservations } from './storage'
import type { TrackedReservation } from './types'

export default function App() {
  const [itemId, setItemId] = useState('item_4021')
  const [refreshKey, setRefreshKey] = useState(0)
  const [reservations, setReservations] = useState<TrackedReservation[]>(loadReservations)

  const hasActive = reservations.some((r) => r.status === 'active')
  const now = useNow(hasActive)

  useEffect(() => {
    saveReservations(reservations)
  }, [reservations])

  const bumpStock = useCallback(() => setRefreshKey((k) => k + 1), [])

  return (
    <div className="board">
      <header className="board-header">
        <h1>Flash Sale Stock Board</h1>
        <p>Reserve stock, confirm before the countdown runs out, or the units go back.</p>
      </header>

      <div className="grid">
        <StockPanel itemId={itemId} onItemIdChange={setItemId} refreshKey={refreshKey} />
        <ReserveForm
          itemId={itemId}
          onReserved={(r) => setReservations((prev) => [...prev, r])}
          onAttempt={bumpStock}
        />
      </div>

      <ReservationList
        reservations={reservations}
        now={now}
        onChange={setReservations}
        onSettled={bumpStock}
      />
    </div>
  )
}
