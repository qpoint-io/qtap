// Test script to see what import.meta.glob returns
const icons = import.meta.glob('../src/assets/icons/**/*.svg', { 
  eager: false, 
  query: '?raw' 
})
console.log('Keys:', Object.keys(icons))
