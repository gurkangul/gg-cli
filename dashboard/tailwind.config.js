/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        bg: '#0d1117',
        panel: '#161b22',
        panel2: '#1c2330',
        border: '#2a3340',
        fg: '#e6edf3',
        dim: '#8b949e',
        accent: '#58a6ff',
        good: '#3fb950',
        bad: '#f85149',
        warn: '#d29922',
        purple: '#bc8cff',
      },
    },
  },
  plugins: [],
}
