export function genRandPassword(pwLen = 15) {
    const chars = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz';
    return Array(pwLen).fill(chars).map(c => c[Math.floor(Math.random() * c.length)]).join('');
}

export default genRandPassword;
