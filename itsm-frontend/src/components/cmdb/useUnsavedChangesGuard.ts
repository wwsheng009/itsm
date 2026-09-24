
import { useCallback, useEffect, useRef } from 'react';
import { Modal } from 'antd';
import type { NavigateFunction } from 'react-router';

/** 拦截未保存表单的离开行为；接受 react-router 的 navigate（迁移自 Next useRouter）。 */
export function useUnsavedChangesGuard(navigate: NavigateFunction) {
  const dirtyRef = useRef(false);
  const confirmingRef = useRef(false);

  const confirmLeave = useCallback((leave: () => void) => {
    if (!dirtyRef.current) {
      leave();
      return;
    }
    if (confirmingRef.current) return;
    confirmingRef.current = true;
    Modal.confirm({
      title: '确认离开？',
      content: '表单内容尚未保存，离开后将丢失已填写的内容。',
      okText: '离开',
      okButtonProps: { danger: true },
      cancelText: '继续编辑',
      onOk: () => {
        dirtyRef.current = false;
        leave();
      },
      afterClose: () => {
        confirmingRef.current = false;
      },
    });
  }, []);

  useEffect(() => {
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!dirtyRef.current) return;
      event.preventDefault();
      event.returnValue = '';
    };
    const handleDocumentClick = (event: MouseEvent) => {
      if (!dirtyRef.current || event.defaultPrevented || event.button !== 0) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
      const anchor = (event.target as Element | null)?.closest('a[href]');
      if (!(anchor instanceof HTMLAnchorElement) || anchor.target === '_blank') return;
      const target = new URL(anchor.href, window.location.href);
      if (target.origin !== window.location.origin || target.href === window.location.href) return;
      event.preventDefault();
      event.stopPropagation();
      confirmLeave(() => navigate(`${target.pathname}${target.search}${target.hash}`));
    };
    const handlePopState = () => {
      if (!dirtyRef.current || confirmingRef.current) return;
      window.history.forward();
      confirmLeave(() => window.setTimeout(() => window.history.back(), 0));
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    window.addEventListener('popstate', handlePopState);
    document.addEventListener('click', handleDocumentClick, true);
    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload);
      window.removeEventListener('popstate', handlePopState);
      document.removeEventListener('click', handleDocumentClick, true);
    };
  }, [confirmLeave, navigate]);

  return {
    markDirty: () => {
      dirtyRef.current = true;
    },
    clearDirty: () => {
      dirtyRef.current = false;
    },
    handleCancel: () => confirmLeave(() => navigate(-1)),
  };
}
