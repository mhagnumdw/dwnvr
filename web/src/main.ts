import './app.css';
import { mount } from 'svelte';
import App from './App.svelte';
// Loaded at boot, and not with My account: the `system` theme follows the
// device on any screen.
import './lib/theme.svelte.ts';

const target = document.getElementById('app');
if (!target) throw new Error('o index.html não tem o #app');

export default mount(App, { target });
