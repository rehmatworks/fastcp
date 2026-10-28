export type ThemeChoice = 'system' | 'light' | 'dark';

const KEY = 'fastcp.theme';

function read(): ThemeChoice {
  try {
    const v = localStorage.getItem(KEY);
    return v === 'light' || v === 'dark' ? v : 'system';
  } catch {
    return 'system';
  }
}

class Theme {
  choice = $state<ThemeChoice>(typeof localStorage === 'undefined' ? 'system' : read());

  set(choice: ThemeChoice): void {
    this.choice = choice;
    const root = document.documentElement;
    if (choice === 'system') root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', choice);
    try {
      if (choice === 'system') localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, choice);
    } catch {
      // ignore
    }
  }
}

export const theme = new Theme();
