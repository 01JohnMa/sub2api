/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // coococode lagoon primary
        primary: {
          50: '#edf8f5',
          100: '#d5ede8',
          200: '#addbd3',
          300: '#7fc4ba',
          400: '#47a79d',
          500: '#0f7d73',
          600: '#0b6a62',
          700: '#0a5650',
          800: '#0b4540',
          900: '#0a3935',
          950: '#062421'
        },
        // coococode reef/coconut secondary
        accent: {
          50: '#f4f7f5',
          100: '#e7eeea',
          200: '#d4e1db',
          300: '#b4cbc4',
          400: '#84aaa2',
          500: '#5c8e86',
          600: '#456f69',
          700: '#355953',
          800: '#2a4742',
          900: '#243b37',
          950: '#17231f'
        },
        reef: {
          50: '#edf6fb',
          100: '#d8eaf5',
          200: '#b6d8eb',
          300: '#86bddb',
          400: '#4e99c3',
          500: '#1d5d8f',
          600: '#174f7a',
          700: '#143f62',
          800: '#123550',
          900: '#102d44'
        },
        coconut: {
          50: '#faf7f3',
          100: '#f0e8dd',
          200: '#dfccb8',
          300: '#c6aa8f',
          400: '#aa8567',
          500: '#8a6a4f',
          600: '#71533d',
          700: '#5b4130',
          800: '#493529',
          900: '#3d2d24'
        },
        coral: {
          50: '#fdf3ef',
          100: '#f9e0d7',
          200: '#f3beb0',
          300: '#eb927d',
          400: '#d96c4a',
          500: '#c45133',
          600: '#a33e27',
          700: '#823222',
          800: '#6c2d22',
          900: '#5a2921'
        },
        // palm dark mode background
        dark: {
          50: '#f4f7f5',
          100: '#e3ebe7',
          200: '#c9d8d1',
          300: '#a7bfb5',
          400: '#78998d',
          500: '#587c70',
          600: '#426259',
          700: '#304d45',
          800: '#213a34',
          900: '#172b26',
          950: '#0b1714'
        }
      },
      fontFamily: {
        sans: [
          'Avenir Next',
          'Noto Sans SC',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['JetBrains Mono', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 12px 40px rgba(15, 61, 54, 0.08)',
        'glass-sm': '0 6px 20px rgba(15, 61, 54, 0.06)',
        glow: '0 0 0 1px rgba(15, 125, 115, 0.16), 0 12px 28px rgba(15, 125, 115, 0.16)',
        'glow-lg': '0 0 0 1px rgba(15, 125, 115, 0.18), 0 20px 50px rgba(15, 125, 115, 0.2)',
        card: '0 1px 2px rgba(23, 35, 31, 0.06), 0 10px 28px rgba(23, 35, 31, 0.05)',
        'card-hover': '0 16px 42px rgba(23, 35, 31, 0.1)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.45)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #0f7d73 0%, #1d5d8f 100%)',
        'gradient-dark': 'linear-gradient(135deg, #123b35 0%, #0b1714 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(248,250,247,0.88) 0%, rgba(238,246,242,0.72) 100%)',
        'mesh-gradient':
          'linear-gradient(180deg, rgba(248,250,247,0.92) 0%, rgba(237,248,245,0.76) 45%, rgba(244,247,245,0.92) 100%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(20, 184, 166, 0.25)' },
          '100%': { boxShadow: '0 0 30px rgba(20, 184, 166, 0.4)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '1rem'
      }
    }
  },
  plugins: []
}
