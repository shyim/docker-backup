/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./internal/dashboard/templates/**/*.templ",
  ],
  darkMode: 'media',
  theme: {
    extend: {
      colors: {
        primary: '#2563eb',
      }
    }
  },
  plugins: [],
}
