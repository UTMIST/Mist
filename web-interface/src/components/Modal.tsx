import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'

/** Native dialog provides focus trapping, focus return and the modal top layer. */
export function Modal({
  title,
  onClose,
  children,
  closeDisabled = false,
}: {
  title: string
  onClose: () => void
  children: ReactNode
  closeDisabled?: boolean
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const current = dialog.current
    current?.showModal()
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      current?.close()
      document.body.style.overflow = previous
    }
  }, [])
  return createPortal(
    <dialog
      ref={dialog}
      className="mist-content mist-modal"
      aria-label={title}
      onCancel={(event) => {
        event.preventDefault()
        if (!closeDisabled) onClose()
      }}
      onClick={(event) => {
        if (!closeDisabled && event.target === event.currentTarget) onClose()
      }}
    >
      <div className="mist-modal-heading">
        <strong>{title}</strong>
        <button
          type="button"
          disabled={closeDisabled}
          aria-label={`Close ${title}`}
          onClick={onClose}
        >
          <X size={18} />
        </button>
      </div>
      <div className="mist-modal-body">{children}</div>
    </dialog>,
    document.body,
  )
}
