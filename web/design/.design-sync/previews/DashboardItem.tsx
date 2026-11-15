// @dsCard group="Dashboard"
import { DashboardGrid, DashboardItem, Widget } from '../../src';

/** One placed widget: 2 wide, 1 tall, at column 5. In the editor the selected item has the focus ring. */
export function Selected() {
  return (
    <DashboardGrid editing aria-label="Layout">
      <DashboardItem x={0} y={0} w={4} h={1}>
        <Widget title="Today">Chores and routines</Widget>
      </DashboardItem>
      <DashboardItem x={4} y={0} w={2} h={1} selected>
        <Widget title="Meals">Pasta night</Widget>
      </DashboardItem>
    </DashboardGrid>
  );
}
