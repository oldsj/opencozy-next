// @dsCard group="People"
import { ProfileAvatar, profileColors } from '../../src';

const names = ['Ada', 'Bea', 'Cal', 'Dev', 'Eli', 'Fin', 'Gus', 'Hal'];

export function Palette() {
  return (
    <div className="flex flex-wrap gap-3">
      {profileColors.map((color, i) => (
        <ProfileAvatar key={color} name={names[i] ?? color} color={color} />
      ))}
    </div>
  );
}

export function Sizes() {
  return (
    <div className="flex items-center gap-3">
      <ProfileAvatar name="Noor" color="lilac" size="small" />
      <ProfileAvatar name="Noor" color="lilac" />
      <ProfileAvatar name="Noor" color="lilac" size="large" />
    </div>
  );
}
