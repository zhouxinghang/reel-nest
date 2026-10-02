import { useEffect, useRef, type ReactNode } from "react";
import styles from "./dialog.module.css";

// Native modal supplies focus containment and restores the invoking control.
export function Dialog({
  title,
  onClose,
  children,
  wide = false,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    const dialog = ref.current!;
    const prior = document.activeElement as HTMLElement | null;
    const overflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    dialog.showModal();
    return () => {
      dialog.close();
      document.body.style.overflow = overflow;
      prior?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className={styles.dialog}
      data-wide={wide}
      aria-label={title}
      onCancel={(event) => {
        event.preventDefault();
        close.current();
      }}
    >
      <header className={styles.header}>
        <h2>{title}</h2>
        <button aria-label={`关闭${title}`} onClick={onClose}>
          ×
        </button>
      </header>
      {children}
    </dialog>
  );
}
