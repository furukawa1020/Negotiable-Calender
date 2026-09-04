import { fireEvent, render, screen, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import SharingPolicyEditor, { sharingPolicyError, type SharingPolicyDraft } from './SharingPolicyEditor'

const policy = (): SharingPolicyDraft => ({
  default: {
    availability: 'available',
    interruptibility: 'normal',
    requestability: 'open',
    reschedulability: 'medium',
  },
  workingHours: [{ weekday: 1, startMinute: 540, endMinute: 1080 }],
  rules: [],
})

function Harness({ initial = policy() }: { initial?: SharingPolicyDraft }) {
  const [value, setValue] = useState(initial)
  return (
    <>
      <SharingPolicyEditor value={value} onChange={setValue} />
      <output aria-label="policy-json">{JSON.stringify(value)}</output>
    </>
  )
}

describe('SharingPolicyEditor', () => {
  it('rejects overlapping windows and malformed bounded rules', () => {
    expect(sharingPolicyError({
      ...policy(),
      workingHours: [
        { weekday: 1, startMinute: 540, endMinute: 720 },
        { weekday: 1, startMinute: 660, endMinute: 780 },
      ],
    })).toContain('重複')

    expect(sharingPolicyError({
      ...policy(),
      rules: [{
        conditionType: 'calendar',
        condition: { calendarId: '' },
        state: policy().default,
        priority: 100,
        enabled: true,
      }],
    })).toContain('Calendar ID')

    expect(sharingPolicyError({
      ...policy(),
      rules: [{
        conditionType: 'organization',
        condition: {},
        state: policy().default,
        priority: 1001,
        enabled: true,
      }],
    })).toContain('0〜1000')
  })

  it('edits every state dimension, working hours, and typed rule conditions', () => {
    render(<Harness />)

    fireEvent.change(screen.getByLabelText('基本の公開状態'), { target: { value: 'limited' } })
    fireEvent.change(screen.getByLabelText('基本の割り込み'), { target: { value: 'urgent_only' } })
    fireEvent.change(screen.getByLabelText('基本の依頼受付'), { target: { value: 'async_only' } })
    fireEvent.change(screen.getByLabelText('基本の変更しやすさ'), { target: { value: 'low' } })

    fireEvent.click(screen.getAllByRole('button', { name: '＋ 時間帯' })[5])
    fireEvent.change(screen.getByLabelText('土曜日 勤務開始 1'), { target: { value: '10:00' } })
    fireEvent.change(screen.getByLabelText('土曜日 勤務終了 1'), { target: { value: '14:00' } })

    fireEvent.click(screen.getByRole('button', { name: '＋ 条件別ルールを追加' }))
    fireEvent.change(screen.getByLabelText('ルール1の条件'), { target: { value: 'calendar' } })
    fireEvent.change(screen.getByLabelText('ルール1のCalendar ID'), { target: { value: 'primary-id@example.com' } })
    fireEvent.change(screen.getByLabelText('ルール1の優先度'), { target: { value: '250' } })
    fireEvent.change(screen.getByLabelText('ルール1の空き状態'), { target: { value: 'unavailable' } })

    expect(JSON.parse(screen.getByLabelText('policy-json').textContent ?? '')).toEqual(expect.objectContaining({
      default: {
        availability: 'limited',
        interruptibility: 'urgent_only',
        requestability: 'async_only',
        reschedulability: 'low',
      },
      workingHours: expect.arrayContaining([
        expect.objectContaining({ weekday: 6, startMinute: 600, endMinute: 840 }),
      ]),
      rules: [expect.objectContaining({
        conditionType: 'calendar',
        condition: { calendarId: 'primary-id@example.com' },
        priority: 250,
        state: expect.objectContaining({ availability: 'unavailable' }),
      })],
    }))
  })

  it('keeps the privacy preview limited to interaction state', () => {
    render(<Harness initial={{
      ...policy(),
      rules: [{
        conditionType: 'calendar',
        condition: { calendarId: 'secret-board-calendar@example.com' },
        state: policy().default,
        priority: 100,
        enabled: true,
      }],
    }} />)

    const preview = screen.getByRole('region', { name: '組織への公開プレビュー' })
    expect(within(preview).getByText('相談可能')).toBeInTheDocument()
    expect(preview).not.toHaveTextContent('secret-board-calendar@example.com')
    expect(preview).toHaveTextContent('予定名・説明・場所・参加者・Calendar名は表示されません')
  })
})
