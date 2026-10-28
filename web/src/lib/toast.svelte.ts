export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

class Toasts {
  items = $state<Toast[]>([]);
  #next = 1;

  show(kind: ToastKind, message: string, timeout = kind === 'error' ? 8000 : 4000): void {
    const id = this.#next++;
    this.items = [...this.items.filter((t) => t.message !== message), { id, kind, message }].slice(-4);
    if (timeout > 0) setTimeout(() => this.dismiss(id), timeout);
  }

  success(message: string): void {
    this.show('success', message);
  }

  error(message: string): void {
    this.show('error', message);
  }

  info(message: string): void {
    this.show('info', message);
  }

  dismiss(id: number): void {
    this.items = this.items.filter((t) => t.id !== id);
  }
}

export const toast = new Toasts();
