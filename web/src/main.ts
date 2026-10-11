import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';

const alvo = document.getElementById('app');
if (!alvo) throw new Error('o index.html não tem o #app');

export default mount(App, { target: alvo });
