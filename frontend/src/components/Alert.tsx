import { ApiError } from '../api/client'

const READABLE: Record<string, string> = {
  INVALID_INPUT: 'the request was rejected before it reached the database',
  INVALID_JSON: 'the request body was not valid json',
  ITEM_NOT_FOUND: 'no inventory item carries that id',
  INSUFFICIENT_STOCK: 'not enough units left to cover that quantity',
  RESERVATION_NOT_FOUND: 'that reservation id does not exist',
  ALREADY_CONFIRMED: 'that reservation is already paid for',
  RESERVATION_EXPIRED: 'the countdown finished and the units went back to stock',
  TIMEOUT: 'the database did not answer in time',
  STOCK_INVARIANT: 'the inventory counters disagree with the reservations on file',
  NETWORK: 'the stock service is not running',
}

type Props = {
  error: Error
}

export default function Alert({ error }: Props) {
  if (!(error instanceof ApiError)) {
    return <p className="alert">{error.message}</p>
  }

  const detail = error.field ? `${error.field} ${error.reason ?? ''}` : error.requestId
  return (
    <p className="alert">
      {READABLE[error.code] ?? error.message}
      <span className="alert-code">
        {error.code}
        {detail ? ` · ${detail}` : ''}
      </span>
    </p>
  )
}
