// @dsCard group="Forms"
import { Field } from '../../src';

export function Form() {
  return (
    <div className="grid max-w-md gap-5">
      <Field label="Name" htmlFor="preview-name">
        <input id="preview-name" defaultValue="Tablet time" />
      </Field>
      <Field label="Cost in points" htmlFor="preview-cost">
        <input id="preview-cost" type="number" defaultValue={20} />
      </Field>
      <Field label="Screen time" htmlFor="preview-target" hint="Unlocks the device as soon as it is chosen.">
        <select id="preview-target" defaultValue="ipad">
          <option value="">No, a treat or activity</option>
          <option value="ipad">Yes, iPad time</option>
        </select>
      </Field>
    </div>
  );
}
