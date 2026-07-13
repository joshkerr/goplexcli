/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ink: {
          900: "#090b11",
          800: "#0d1017",
          750: "#10141e",
          700: "#141925",
          600: "#1b2230",
          500: "#27303f",
        },
        accent: {
          DEFAULT: "#e5a00d",
          soft: "#f5c451",
          dark: "#b87d00",
        },
      },
      fontFamily: {
        sans: [
          "Inter",
          "-apple-system",
          "BlinkMacSystemFont",
          "Segoe UI",
          "Roboto",
          "Helvetica Neue",
          "Arial",
          "sans-serif",
        ],
      },
      boxShadow: {
        card: "0 10px 30px -12px rgba(0,0,0,0.6)",
        glow: "0 0 0 1px rgba(229,160,13,0.4), 0 8px 24px -6px rgba(229,160,13,0.35)",
      },
      keyframes: {
        "fade-in": {
          "0%": { opacity: "0", transform: "translateY(6px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
      },
      animation: {
        "fade-in": "fade-in 0.18s ease-out",
      },
    },
  },
  plugins: [],
};
