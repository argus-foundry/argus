import {menuIcon} from '../shared/menu-icon.js';

export function settingsIcon(id, label) {
  if(id.startsWith('repository:')){
    const avatar=document.createElement('span');avatar.className='settings-nav-icon';avatar.setAttribute('aria-hidden','true');
    avatar.textContent=label.slice(0,1).toUpperCase();return avatar;
  }
  return menuIcon({general:'settings','add-repository':'add'}[id] || id);
}
