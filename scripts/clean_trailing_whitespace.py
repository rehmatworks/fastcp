"""
Small script to normalize trailing whitespace and ensure files end with a
single newline and no blank lines at EOF (fixes W293/W391).
"""
import pathlib

ROOT = pathlib.Path(__file__).resolve().parents[1]
EXCLUDE = {'.venv', 'node_modules', 'staticfiles', '__pycache__'}

for p in ROOT.rglob('*.py'):
    if any(part in EXCLUDE for part in p.parts):
        continue
    text = p.read_text(encoding='utf-8')
    # split into lines, strip trailing whitespace per line
    lines = [line.rstrip() for line in text.splitlines()]
    # remove trailing empty lines so file doesn't end with a blank line
    while lines and lines[-1] == '':
        lines.pop()
    # ensure single newline at EOF
    new_text = '\n'.join(lines) + '\n'
    if new_text != text:
        p.write_text(new_text, encoding='utf-8')
        print('fixed', p)
